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
	"time"
)

const facebookURL string = "https://mbasic.facebook.com"
const facebookLoginURL string = "https://mbasic.facebook.com/login/device-based/regular/login/"
const profileURL string = "https://mbasic.facebook.com/profile"
const activityURL string = "https://mbasic.facebook.com/<profileid>/allactivity"

var yearOptions = []string{"2020", "2019", "2018", "2017", "2016", "2015", "2014", "2013", "2012", "2011", "2010", "2009", "2008", "2007", "2006"}
var monthStrings = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
var categoriesMap = map[string]string{
	"Comments":            "commentscluster",
	"Posts":               "statuscluster",
	"Likes and Reactions": "likes",
	"Search History":      "search",
}

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

type activityReader struct {
	req        *requester
	fbl        *fbLogin
	deleteURLs []string
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
		actRead.StoreItemsFromOutput(output)
		actRead.UpdateOutputRead(month)

		requestURL = strings.SplitAfter(output, searchString)[0]
		requestURL = facebookURL + requestURL[strings.LastIndex(requestURL, `"`)+1:]
		output = actRead.req.Request(requestURL)
		moreCounter++
	}
}

func (actRead *activityReader) StoreItemsFromOutput(out string) {
	token := "action=unlike"
	// token := "deletion_request_id"
	var match int
	var from int
	var to int

	for {
		match = strings.Index(out, token)
		if match == -1 {
			break
		}
		from = strings.LastIndex(out[:match], `"`) + 1
		to = match + strings.Index(out[match:], `"`)
		actRead.deleteURLs = append(actRead.deleteURLs, facebookURL+out[from:to])
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
	str += "  Elements found:\t" + strconv.Itoa(len(actRead.deleteURLs))
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
	survey.MultiSelectQuestionTemplate = strings.Replace(survey.MultiSelectQuestionTemplate, "enter to select, type to filter", "space to select, type to filter, enter to continue", 1)
	prompt := &survey.MultiSelect{
		Message:  "Which " + yearsOrCategories + " do you want to delete from:",
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

func (actRead *activityReader) ReadYearsAndCategories(years []string, categories []string) {
	for _, year := range years {
		fmt.Println("Searching elements from " + year + ":")
		yearInt, _ := strconv.Atoi(year)
		for i := 1; i <= 12; i++ {
			actRead.UpdateOutputRead(i)
			for _, category := range categories {
				actRead.ReadItems(yearInt, i, category)
			}
		}
		fmt.Println("\nDeleting elements: from " + year + ":")
		bar := pb.Full.Start(len(actRead.deleteURLs))
		for _, deleteURL := range actRead.deleteURLs {
			actRead.req.Request(deleteURL)
			bar.Increment()
		}
		bar.Finish()
	}
}

func main() {
	req := newRequester()
	fbl := newFbLogin(req)
	actRead := activityReader{req, fbl, make([]string, 0)}

	years := createMultiSelect("years", yearOptions)
	categories := createMultiSelect("categories", categorySlice())
	actRead.ReadYearsAndCategories(years, categories)
}
