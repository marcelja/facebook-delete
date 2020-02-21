//usr/bin/env go run $0 "$@"; exit
package main

import (
	"bufio"
	"fmt"
	cookiejar "github.com/juju/persistent-cookiejar"
	"golang.org/x/crypto/ssh/terminal"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const facebookUrl string = "https://mbasic.facebook.com"
const facebookLoginUrl string = "https://mbasic.facebook.com/login/device-based/regular/login/"
const profileUrl string = "https://mbasic.facebook.com/profile"
const activityUrl string = "https://mbasic.facebook.com/<profileid>/allactivity"

type requester struct {
	client *http.Client
	jar    *cookiejar.Jar
}

func NewRequester() *requester {
	req := new(requester)
	req.jar, _ = cookiejar.New(&cookiejar.Options{})
	req.client = &http.Client{Jar: req.jar}
	return req
}

func (r *requester) Request(requestUrl string) string {
	resp, err := r.client.Get(requestUrl)
	return RetrieveRequestString(resp, err)
}

func (r *requester) RequestPostForm(requestUrl string, form url.Values) string {
	resp, err := r.client.PostForm(requestUrl, form)
	return RetrieveRequestString(resp, err)
}

func RetrieveRequestString(resp *http.Response, err error) string {
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
	profileId string
}

func NewFbLogin(req *requester) *fbLogin {
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
	fbl.requester.RequestPostForm(facebookLoginUrl, form)
}

func (fbl *fbLogin) IsLoggedIn() bool {
	output := fbl.requester.Request(profileUrl)
	if strings.Contains(output, `name="sign_up"`) {
		return false
	}
	PrintUserName(output)
	fbl.StoreProfileId(output)
	return true
}

func (fbl *fbLogin) StoreProfileId(output string) {
	result := strings.Split(output, ";profile_id=")[1]
	result = strings.Split(result, "&amp;")[0]
	fbl.profileId = result
	fmt.Println("Profile ID:", fbl.profileId)
}

func PrintUserName(output string) {
	result := strings.Split(output, `<title>`)[1]
	result = strings.Split(result, `</title`)[0]
	fmt.Println("Logged in with user:", result)
}

type activityReader struct {
	req        *requester
	fbl        *fbLogin
	deleteUrls []string
}

func (actRead *activityReader) readItems(year int, month int) {
	requestUrl, sectionIdStr := CreateRequestUrl(year, month, actRead.fbl.profileId)
	output := actRead.req.Request(requestUrl)

	moreCounter := 1
	var searchString string
	for {
		searchString = sectionIdStr + `_more_` + strconv.Itoa(moreCounter)
		if !strings.Contains(output, searchString) {
			break
		}
		actRead.storeItemsFromOutput(output)

		requestUrl = strings.SplitAfter(output, searchString)[0]
		requestUrl = facebookUrl + requestUrl[strings.LastIndex(requestUrl, `"`)+1:]
		// TODO move to requester
		requestUrl = strings.Replace(requestUrl, "&amp;", "&", -1)
		output = actRead.req.Request(requestUrl)
		moreCounter += 1
	}
}

func (actRead *activityReader) storeItemsFromOutput(out string) {
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
		actRead.deleteUrls = append(actRead.deleteUrls, facebookUrl+out[from:to])
		out = out[to:]
	}
}

func (actRead *activityReader) readYear(year int) {
	for i := 1;  i<=12; i++ {
		actRead.readItems(year, i)
		fmt.Println("Number of delete urls in slice:", len(actRead.deleteUrls))
	}
}

func CreateRequestUrl(year int, month int, profileId string) (string, string) {
	sectionIdStr := "sectionID=month_" + strconv.Itoa(year) + "_" + strconv.Itoa(month)
	newUrl := strings.Replace(activityUrl, "<profileid>", profileId, 1)
	// TODO variable category key
	newUrl += "?category_key=likes"
	newUrl += "&timeend=" + ToUnixTime(year, month+1, 1)
	newUrl += "&timestart=" + ToUnixTime(year, month, 0)
	newUrl += "&" + sectionIdStr
	return newUrl, sectionIdStr
}

func ToUnixTime(year int, month int, decrement int64) string {
	location, _ := time.LoadLocation("America/Los_Angeles")
	timestamp := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
	return strconv.FormatInt(timestamp.Unix()-decrement, 10)
}

func main() {
	req := NewRequester()
	fbl := NewFbLogin(req)
	actRead := activityReader{req, fbl, make([]string, 0)}
	// actRead.readItems(2020, 2)
	actRead.readYear(2011)
	// fmt.Println(actRead.deleteUrls)
	// actRead.readItems(2020, 1)
	// actRead.readItems(2011, 5)
}
