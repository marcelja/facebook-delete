//usr/bin/env go run $0 "$@"; exit
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/cheggaaa/pb/v3"
	"github.com/juju/persistent-cookiejar"
)

const numRoutines int = 5
const facebookURL string = "https://mbasic.facebook.com"
const facebookLoginURL string = "https://mbasic.facebook.com/login/device-based/regular/login/"
const profileURL string = "https://mbasic.facebook.com/profile"
const activityURL string = "https://mbasic.facebook.com/<profileid>/allactivity"

var yearOptions = []string{"2024", "2023", "2022", "2021", "2020", "2019", "2018", "2017", "2016", "2015", "2014", "2013", "2012", "2011", "2010", "2009", "2008", "2007", "2006"}
var monthStrings = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
var categoriesMap = map[string]string{
	"Comments":                       "commentscluster",
	"Posts":                          "statuscluster",
	"Likes and Reactions":            "likes",
	"Search History":                 "search",
	"Event Responses":                "eventrsvps",
	"Your Events":                    "createdevents",
	"Event Invitations":              "invitedevents",
	"Photos and Videos":              "photos",
	"Group Posts and Comments":       "groupposts",
	"Group Reactions":                "groupreactions",
	"Others' Posts To Your Timeline": "wallcluster",
	"Posts You're Tagged In":         "tagsbyotherscluster",
	"All App Activity":               "allapps",
	"Instagram Photos and Videos":    "genericapp&category_app_id=124024574287414",
	"Spotify":                        "genericapp&category_app_id=174829003346",
}

var tokensInURLs = [...]string{"/removecontent", "/delete", "/report", "/events/remove.php", "&amp;content_type=4&amp;", "action=delete"}

var rateLimit int
var limitSearch bool
var limitDelete bool
var customYears string
var customMonths string
var selectAllContent bool

type requester struct {
	client *http.Client
	jar    *cookiejar.Jar
}

func newRequester() *requester {
	req := new(requester)
	req.jar, _ = cookiejar.New(&cookiejar.Options{})
	req.client = &http.Client{Jar: req.jar}
	return req
}

func (r *requester) Request(requestURL string) string {
	requestURL = updateURL(requestURL)
	resp, err := r.client.Get(requestURL)
	if resp.StatusCode != 200 {
		panic("bad response status")
	}
	return retrieveRequestString(resp, err)
}

func (r *requester) RequestPostForm(requestURL string, form url.Values) string {
	requestURL = updateURL(requestURL)
	resp, err := r.client.PostForm(requestURL, form)
	if resp.StatusCode != 200 {
		panic("bad response status")
	}
	return retrieveRequestString(resp, err)
}

func updateURL(requestURL string) string {
	return strings.Replace(requestURL, "&amp;", "&", -1)
}

func retrieveRequestString(resp *http.Response, err error) string {
	if err != nil {
		fmt.Println("error during http request")
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("error during http request")
	}
	strBody := string(body)
	if strings.Contains(strBody, "You can try again later") || strings.Contains(strBody, "temporarily blocked") {
		panic("Ratelimited, please open https://mbasic.facebook.com and navigate to your activity log to see more information.")
	}
	return strBody
}

type fbLogin struct {
	requester *requester
	email     string
	password  string
	profileID string
}

func newFbLogin(req *requester) *fbLogin {
	fbl := new(fbLogin)
	fbl.requester = req

	if !fbl.IsLoggedIn() {
		fbl.EnterInformation()
		fbl.Login()
		req.jar.Save()
		if !fbl.IsLoggedIn() {
			panic("Failed to login. Please open https://mbasic.facebook.com in a browser and login there. Facebook might requires an additional verification. Afterwards, you can try again.")
		}
	}
	return fbl
}

func (fbl *fbLogin) EnterInformation() {
	email := ""
	prompt := &survey.Input{
		Message: "Please type your email",
	}
	survey.AskOne(prompt, &email)

	password := ""
	promptPW := &survey.Password{
		Message: "Please type your password",
	}
	survey.AskOne(promptPW, &password)

	fbl.email = email
	fbl.password = password
}

func (fbl *fbLogin) Login() {
	fmt.Println("Attempting Login...")
	form := url.Values{
		"email": {fbl.email},
		"pass":  {fbl.password},
		"login": {"Log In"},
	}
	// This first request is expected to fail
	fbl.requester.RequestPostForm(facebookLoginURL, form)

	loginFormHTML := fbl.requester.Request(facebookURL)
	lsdToken := readLoginToken(loginFormHTML, "lsd")
	jazoestToken := readLoginToken(loginFormHTML, "jazoest")
	liToken := readLoginToken(loginFormHTML, "li")

	form = url.Values{
		"email":   {fbl.email},
		"pass":    {fbl.password},
		"lsd":     {lsdToken},
		"jazoest": {jazoestToken},
		"li":      {liToken},
		"login":   {"Log In"},
	}
	fbl.requester.RequestPostForm(facebookLoginURL, form)
}

func (fbl *fbLogin) IsLoggedIn() bool {
	output := fbl.requester.Request(profileURL)
	if strings.Contains(output, `name="sign_up"`) {
		return false
	}
	fbl.StoreProfileID(output)
	fbl.PrintUserName(output)
	return true
}

func (fbl *fbLogin) StoreProfileID(output string) {
	slice := strings.Split(output, ";profile_id=")
	if len(slice) == 1 {
		fbl.requester.jar.RemoveAll()
		panic(fmt.Sprintf("Unable to parse profile information from cookies. Try manually deleting the cookie file stored in %s", cookiejar.DefaultCookieFile()))
	}
	result := strings.Split(output, ";profile_id=")[1]
	result = strings.Split(result, "&amp;")[0]
	fbl.profileID = result
}

func (fbl *fbLogin) PrintUserName(output string) {
	result := strings.Split(output, `<title>`)[1]
	result = strings.Split(result, `</title`)[0]
	fmt.Println("Logged in with user:", result, "(profile ID:", fbl.profileID+")")
}

type deleteElement struct {
	URL      string
	success  bool
	category string
	token    string
}

type activityReader struct {
	req            *requester
	fbl            *fbLogin
	deleteElements []deleteElement
	selectedMonths []string
}

func (actRead *activityReader) ReadItems(year int, month int, category string) {
	requestURL, sectionIDStr := createRequestURL(year, month, actRead.fbl.profileID, category)
	output := actRead.req.Request(requestURL)

	moreCounter := 1
	var searchString string
	for {
		if limitSearch {
			time.Sleep(time.Duration(rateLimit) * time.Millisecond)
		}
		actRead.StoreItemsFromOutput(output, category)

		searchString = sectionIDStr + `_more_` + strconv.Itoa(moreCounter)
		if !strings.Contains(output, searchString) {
			break
		}
		actRead.UpdateOutputRead(month)

		requestURL = strings.SplitAfter(output, searchString)[0]
		requestURL = facebookURL + requestURL[strings.LastIndex(requestURL, `"`)+1:]
		output = actRead.req.Request(requestURL)
		moreCounter++
	}
}

func (actRead *activityReader) StoreItemsFromOutput(out string, category string) {
	for _, token := range tokensInURLs {
		actRead.StoreItemsWithToken(out, token, category)
	}
}

func getURLFromToString(htmlOut string, token string) (int, int) {
	match := strings.Index(htmlOut, token)
	if match == -1 {
		return -1, -1
	}
	from := strings.LastIndex(htmlOut[:match], `"`) + 1
	to := match + strings.Index(htmlOut[match:], `"`)
	return from, to
}

func (actRead *activityReader) StoreItemsWithToken(out string, token string, category string) {
	var from int
	var to int

	for {
		from, to = getURLFromToString(out, token)
		if from == -1 {
			break
		}
		actRead.deleteElements = append(actRead.deleteElements, deleteElement{
			facebookURL + out[from:to],
			false, category, token})
		out = out[to:]
	}
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func (actRead *activityReader) UpdateOutputRead(month int) bool {
	currentMonthSkip := true
	str := "\r"
	for i, monthString := range monthStrings {
		if month > i {
			if stringInSlice(monthString, actRead.selectedMonths) {
				currentMonthSkip = false
				str += monthString + " "
			} else {
				currentMonthSkip = true
				str += "... "
			}
		} else {
			str += "    "
		}
	}
	str += "  Elements found:\t" + strconv.Itoa(len(actRead.deleteElements)) + "\t"
	fmt.Printf(str)
	return currentMonthSkip
}

func createRequestURL(year int, month int, profileID string, category string) (string, string) {
	sectionIDStr := "section_id=month_" + strconv.Itoa(year) + "_" + strconv.Itoa(month)
	newURL := strings.Replace(activityURL, "<profileid>", profileID, 1)
	newURL += "?category_key=" + categoriesMap[category]
	newURL += "&timeend=" + toUnixTime(year, month+1, 1)
	newURL += "&timestart=" + toUnixTime(year, month, 0)
	newURL += "&" + sectionIDStr
	return newURL, sectionIDStr
}

func toUnixTime(year int, month int, decrement int64) string {
	// Timezone should be PDT but `time.LoadLocation("America/Los_Angeles")` is not working as Windows executable
	// see https://github.com/golang/go/issues/38453
	timestamp := time.Date(year, time.Month(month), 1, 7, 0, 0, 0, time.UTC)
	return strconv.FormatInt(timestamp.Unix()-decrement, 10)
}

func createMultiSelect(yearsOrCategories string, options []string) []string {
	selected := []string{}
	prompt := &survey.MultiSelect{
		Message:  "Which " + yearsOrCategories,
		Options:  options,
		PageSize: 20,
	}
	survey.AskOne(prompt, &selected)
	return selected
}

func categorySlice() []string {
	keys := []string{}
	for key := range categoriesMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type deleter struct {
	actRead *activityReader
	req     *requester
}

func (del *deleter) Delete(years []string, categories []string) {
	var wg sync.WaitGroup

	for _, year := range years {
		fmt.Println("\nSearching elements from " + year + ":")
		yearInt, _ := strconv.Atoi(year)
		for i := 1; i <= 12; i++ {
			skip := del.actRead.UpdateOutputRead(i)
			if skip {
				continue
			}
			for _, category := range categories {
				del.actRead.ReadItems(yearInt, i, category)
			}
		}
		fmt.Println("\nDeleting elements from " + year + ":")
		bar := pb.Full.Start(len(del.actRead.deleteElements))
		wg.Add(numRoutines)

		for i := 0; i < numRoutines; i++ {
			go del.StartRoutine(i, bar, &wg)
		}
		wg.Wait()
		bar.Finish()
		del.PrintSummary(del.actRead.deleteElements)
		del.actRead.deleteElements = make([]deleteElement, 0)
	}
}

func (del *deleter) PrintSummary(deleteElements []deleteElement) {
	var summary = map[string]int{}
	count := 0
	for _, elem := range deleteElements {
		if elem.success {
			if val, ok := summary[elem.category]; ok {
				summary[elem.category] = val + 1
			} else {
				summary[elem.category] = 1
			}
			count += 1
		}
	}
	for _, category := range categorySlice() {
		if val, ok := summary[category]; ok {
			fmt.Println(category + ": " + strconv.Itoa(val) + " deleted")
		}
	}
	fmt.Println("Total: " + strconv.Itoa(count) + " deleted")
}

func (del *deleter) StartRoutine(ID int, bar *pb.ProgressBar, wg *sync.WaitGroup) {
	var index int
	l := len(del.actRead.deleteElements)
	i := 0

	for {
		index = i*numRoutines + ID
		if index >= l {
			break
		}
		del.DeleteElement(&del.actRead.deleteElements[index])
		bar.Increment()
		i++
	}
	wg.Done()
}

func readDtsgTag(htmlOut string) string {
	dtsgSearch := `name="fb_dtsg" value="`
	match := strings.Index(htmlOut, dtsgSearch)
	dtsgFrom := match + len(dtsgSearch)
	dtsgEnd := strings.Index(htmlOut[dtsgFrom:], `"`)
	return htmlOut[dtsgFrom : dtsgFrom+dtsgEnd]
}

func readLoginToken(htmlOut string, name string) string {
	search := `name="` + name + `" value="`
	match := strings.Index(htmlOut, search)
	from := match + len(search)
	end := strings.Index(htmlOut[from:], `"`)
	return htmlOut[from : from+end]
}

func (del *deleter) Untag(elem *deleteElement) {
	out := del.req.Request(elem.URL)
	from, to := getURLFromToString(out, "/nfx/basic")
	if from == -1 {
		return
	}

	// Request "Yes, I'd like to continue filing this report."
	out = del.req.Request(facebookURL + out[from:to])
	from, to = getURLFromToString(out, "/nfx/basic")
	if from == -1 {
		return
	}

	out = del.req.RequestPostForm(facebookURL+out[from:to], url.Values{
		"fb_dtsg": {readDtsgTag(out)},
		"answer":  {"spam"},
	})
	from, to = getURLFromToString(out, "/nfx/basic")
	if from == -1 {
		return
	}

	del.req.RequestPostForm(facebookURL+out[from:to], url.Values{
		"fb_dtsg":    {readDtsgTag(out)},
		"action_key": {"UNTAG"},
		"submit":     {"Submit"},
	})
	elem.success = true
}

func (del *deleter) DeleteCoverOrProfilePhoto(elem *deleteElement) {
	beginStr := "content_id="
	beginIdx := strings.Index(elem.URL, beginStr) + len(beginStr)
	endIdx := strings.Index(elem.URL, elem.token)
	delURL := facebookURL + "/photo.php?fbid=" + elem.URL[beginIdx:endIdx] + "&delete&id=" + del.actRead.fbl.profileID
	out := del.req.Request(delURL)
	from, to := getURLFromToString(out, "/a/photo.php")
	if from == -1 {
		return
	}
	del.req.RequestPostForm(facebookURL+out[from:to], url.Values{
		"fb_dtsg":              {readDtsgTag(out)},
		"confirm_photo_delete": {"1"},
		"photo_delete":         {"Delete"},
	})
	elem.success = true
}

func (del *deleter) DeleteSearch(elem *deleteElement) {

	out := del.req.Request(elem.URL)

	from, to := getURLFromToString(out, "/allactivity/delete/")
	if from == -1 {
		return
	}
	del_url := facebookURL + out[from:to]
	out = del.req.Request(del_url)
	elem.success = true

}

func (del *deleter) DeleteElement(elem *deleteElement) {
	defer func() {
		if e := recover(); e != nil {
			fmt.Println("Failed to delete element", elem)
			elem.success = false
		}
	}()

	if limitDelete {
		time.Sleep(time.Duration(rateLimit) * time.Millisecond)
	}

	if elem.token == "/report" {
		// Removing tags in activity log has to request "Report",
		// then select "It's spam", then "Remove tag"
		del.Untag(elem)
	} else if strings.Contains(elem.token, "content_type") {
		if elem.category == "Photos and Videos" {
			del.DeleteCoverOrProfilePhoto(elem)
		}
	} else if elem.token == "action=delete" {
		del.DeleteSearch(elem)
	} else {
		del.req.Request(elem.URL)
		elem.success = true
	}
}

func validateYearsFlag(flagContent string) bool {
	years := strings.ReplaceAll(flagContent, " ", "")
	re, _ := regexp.Compile("^(([\\d]{4})([,][\\d]{4})*)?$")
	return re.MatchString(years) || flagContent == "all" || flagContent == ""
}

func validateMonthsFlag(flagContent string) bool {
	months := strings.ReplaceAll(flagContent, " ", "")
	re, _ := regexp.Compile("^((0?[1-9]|1[012])([,](0?[1-9]|1[012]))*)?$")
	return re.MatchString(months) || flagContent == "all" || flagContent == ""
}

func main() {
	flag.IntVar(&rateLimit, "rateLimit", 100, "Wait this many milliseconds between requests.")
	flag.BoolVar(&limitSearch, "limitSearch", true, "Rate-limit searching for things to delete.")
	flag.BoolVar(&limitDelete, "limitDelete", true, "Rate-limit deleting things.")
	flag.StringVar(&customYears, "customYears", "", "Comma-separated years (YYYY) to select.")
	flag.StringVar(&customMonths, "customMonths", "", "Comma-separated months (MM) to set.")
	flag.BoolVar(&selectAllContent, "selectAllContent", false, "Don't ask content type, but select all.")
	flag.Parse()
	if rateLimit > 0 {
		if limitSearch && limitDelete {
			fmt.Printf("Waiting %d ms before search and delete requests.\n", rateLimit)
		} else if limitSearch {
			fmt.Printf("Waiting %d ms before search requests.\n", rateLimit)
		} else if limitDelete {
			fmt.Printf("Waiting %d ms before delete requests.\n", rateLimit)
		}
	}

	// Login
	req := newRequester()
	fbl := newFbLogin(req)
	actRead := activityReader{req, fbl, make([]deleteElement, 0), make([]string, 0)}

	// Flags validation

	customYearsFlagValid := validateYearsFlag(customYears)
	customMonthsFlagValid := validateMonthsFlag(customMonths)

	if customYearsFlagValid {
		if customYears == "all" {
			customYears = "2024,2023,2022,2021,2020,2019,2018,2017,2016,2015,2014,2013,2012,2011,2010,2009,2008,2007,2006"
		}
	} else {
		fmt.Println("Invalid years passed through customYears flag.")
		customYears = ""
	}

	if customMonthsFlagValid {
		if customMonths == "all" {
			customMonths = "01,02,03,04,05,06,07,08,09,10,11,12"
		}
	} else {
		fmt.Println("Invalid months passed through customMonths flag.")
		customMonths = ""
	}

	// Data processing

	years := []string{}
	if customYears == "" {
		years = createMultiSelect("years", yearOptions)
	} else {
		years = strings.Split(customYears, ",")
		fmt.Printf("? Which years: %s \n", strings.Join(years, ", "))
	}

	months := []string{}
	if customMonths == "" {
		months = createMultiSelect("months", monthStrings)
		actRead.selectedMonths = months
	} else {
		months = strings.Split(customMonths, ",")
		monthLabels := map[string]string{
			"1":  "Jan",
			"01": "Jan",
			"2":  "Feb",
			"02": "Feb",
			"3":  "Mar",
			"03": "Mar",
			"4":  "Apr",
			"04": "Apr",
			"5":  "May",
			"05": "May",
			"6":  "Jun",
			"06": "Jun",
			"7":  "Jul",
			"07": "Jul",
			"8":  "Aug",
			"08": "Aug",
			"9":  "Sep",
			"09": "Sep",
			"10": "Oct",
			"11": "Nov",
			"12": "Dec",
		}

		newMonths := []string{}
		for _, month := range months {
			if month, found := monthLabels[month]; found {
				newMonths = append(newMonths, month)
			}
		}
		fmt.Printf("? Which months: %s \n", strings.Join(newMonths, ", "))
		actRead.selectedMonths = newMonths
	}

	categories := []string{}
	if selectAllContent {
		categories = categorySlice()
		fmt.Printf("Selected content: %s \n", strings.Join(categories, ", "))
	} else {
		categories = createMultiSelect("categories", categorySlice())
	}

	del := deleter{&actRead, req}
	del.Delete(years, categories)
}
