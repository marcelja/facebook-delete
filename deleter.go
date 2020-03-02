//usr/bin/env go run $0 "$@"; exit
package main

import (
	"bufio"
	"fmt"
	"github.com/AlecAivazis/survey"
	"github.com/cheggaaa/pb/v3"
	"github.com/juju/persistent-cookiejar"
	"golang.org/x/crypto/ssh/terminal"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const numRoutines int = 40
const facebookURL string = "https://mbasic.facebook.com"
const facebookLoginURL string = "https://mbasic.facebook.com/login/device-based/regular/login/"
const profileURL string = "https://mbasic.facebook.com/profile"
const activityURL string = "https://mbasic.facebook.com/<profileid>/allactivity"

var yearOptions = []string{"2020", "2019", "2018", "2017", "2016", "2015", "2014", "2013", "2012", "2011", "2010", "2009", "2008", "2007", "2006"}
var monthStrings = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
var categoriesMap = map[string]string{
	"Comments":                         "commentscluster",
	"Posts":                            "statuscluster",
	"Likes and Reactions":              "likes",
	"Search History":                   "search",
	"Event Responses":                  "eventrsvps",
	"Your Events":                      "createdevents",
	"Event Invitations":                "invitedevents",
	"Photos and Videos":                "photos",
	"Group Posts, Comments, Reactions": "groupposts",
	"Others' Posts To Your Timeline":   "wallcluster",
	"Posts You're Tagged In":           "tagsbyotherscluster",
	"All App Activity":                 "allapps",
	"Instagram Photos and Videos":      "genericapp&category_app_id=124024574287414",
	"Spotify":                          "genericapp&category_app_id=174829003346",
}

var tokensInURLs = [...]string{"/removecontent", "/delete", "/report", "/events/remove.php", "&amp;content_type=4&amp;"}

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
	return retrieveRequestString(resp, err)
}

func (r *requester) RequestPostForm(requestURL string, form url.Values) string {
	requestURL = updateURL(requestURL)
	resp, err := r.client.PostForm(requestURL, form)
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
	return string(body)
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
			panic("Failed to login")
		}
	}
	return fbl
}

func (fbl *fbLogin) EnterInformation() {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter email: ")
	fbl.email, _ = reader.ReadString('\n')
	fmt.Print("Enter password: ")
	bytePassword, _ := terminal.ReadPassword(0)
	fbl.password = string(bytePassword)
	fmt.Println("")
}

func (fbl *fbLogin) Login() {
	fmt.Println("Attempting Login...")
	form := url.Values{
		"email": {fbl.email},
		"pass":  {fbl.password},
		"login": {"Log In"},
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
}

func (actRead *activityReader) ReadItems(year int, month int, category string) {
	requestURL, sectionIDStr := createRequestURL(year, month, actRead.fbl.profileID, category)
	output := actRead.req.Request(requestURL)

	moreCounter := 1
	var searchString string
	for {
		searchString = sectionIDStr + `_more_` + strconv.Itoa(moreCounter)
		if !strings.Contains(output, searchString) {
			break
		}
		actRead.StoreItemsFromOutput(output, category)
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

func (actRead *activityReader) UpdateOutputRead(month int) {
	str := "\r"
	for i, monthString := range monthStrings {
		if month > i {
			str += monthString + " "
		} else {
			str += "    "
		}
	}
	str += "  Elements found:\t" + strconv.Itoa(len(actRead.deleteElements))
	fmt.Printf(str)
}

func createRequestURL(year int, month int, profileID string, category string) (string, string) {
	sectionIDStr := "sectionID=month_" + strconv.Itoa(year) + "_" + strconv.Itoa(month)
	newURL := strings.Replace(activityURL, "<profileid>", profileID, 1)
	newURL += "?category_key=" + categoriesMap[category]
	newURL += "&timeend=" + toUnixTime(year, month+1, 1)
	newURL += "&timestart=" + toUnixTime(year, month, 0)
	newURL += "&" + sectionIDStr
	return newURL, sectionIDStr
}

func toUnixTime(year int, month int, decrement int64) string {
	location, _ := time.LoadLocation("America/Los_Angeles")
	timestamp := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
	return strconv.FormatInt(timestamp.Unix()-decrement, 10)
}

func createMultiSelect(yearsOrCategories string, options []string) []string {
	selected := []string{}
	survey.MultiSelectQuestionTemplate = strings.Replace(survey.MultiSelectQuestionTemplate, "enter to select, type to filter", "space to select, enter to continue", 1)
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
	return keys
}

type deleter struct {
	actRead *activityReader
	req     *requester
}

func (del *deleter) Delete(years []string, categories []string) {
	var wg sync.WaitGroup

	for _, year := range years {
		fmt.Println("Searching elements from " + year + ":")
		yearInt, _ := strconv.Atoi(year)
		for i := 1; i <= 12; i++ {
			del.actRead.UpdateOutputRead(i)
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
		del.actRead.deleteElements = make([]deleteElement, 0)
	}
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

func (del *deleter) DeleteElement(elem *deleteElement) {
	if elem.token == "/report" {
		// Removing tags in activity log has to request "Report",
		// then select "It's spam", then "Remove tag"
		del.Untag(elem)
	} else if strings.Contains(elem.token, "content_type") {
		if elem.category == "Photos and Videos" {
			del.DeleteCoverOrProfilePhoto(elem)
		}
	} else {
		del.req.Request(elem.URL)
		elem.success = true
	}
}

func main() {
	req := newRequester()
	fbl := newFbLogin(req)
	actRead := activityReader{req, fbl, make([]deleteElement, 0)}

	years := createMultiSelect("years", yearOptions)
	categories := createMultiSelect("categories", categorySlice())
	del := deleter{&actRead, req}
	del.Delete(years, categories)
}
