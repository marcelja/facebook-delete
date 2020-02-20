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
	"strings"
)

const facebookUrl string = "https://mbasic.facebook.com/profile"
const facebookLoginUrl string = "https://mbasic.facebook.com/login/device-based/regular/login/"

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
	output := fbl.requester.Request(facebookUrl)
	if strings.Contains(output, `name="sign_up"`) {
		return false
	}
	PrintUserName(output)
	return true
}

func PrintUserName(output string) {
	result := strings.Split(output, `<title>`)[1]
	result = strings.Split(result, `</title`)[0]
	fmt.Println("Logged in with user:", result)
}

func main() {
	req := NewRequester()
	NewFbLogin(req)
}
