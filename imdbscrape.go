package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"time"

	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/PuerkitoBio/goquery"
	"github.com/aws/aws-lambda-go/lambda"
)

var (
	local       bool
	maxNumFilms = 10
)

type (
	movie struct {
		ID         string  `json:"id"`
		ImdbRating float64 `json:"imdbRating"`
	}

	listings struct {
		Movies   []movie `json:"movies"`
		ImdbLink string  `json:"imdbLink"`
	}
)

func main() {
	if os.Getenv("LAMBDA_TASK_ROOT") != "" {
		lambda.Start(handleRequest)
	} else {
		local = true
		handleRequest()
	}
}

func handleRequest() (string, error) {
	// Update limit if env var provided
	mxNumFilms, _ := strconv.Atoi(os.Getenv("MAX_NUM_FILMS"))
	if mxNumFilms > 0 {
		maxNumFilms = mxNumFilms
	}

	// Get start and end date strings
	now := time.Now()
	end := now.Format("2006-01-02")
	start := now.AddDate(0, 0, -6).Format("2006-01-02") // 6 days ago

	// Make IMDB request
	url := fmt.Sprintf("https://www.imdb.com/search/title/?title_type=feature&year=%s,%s&view=advanced", start, end)
	res, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
		return "", nil
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		log.Fatalf("status code error: %d %s", res.StatusCode, res.Status)
		return "", nil
	}

	// Load the HTML document
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Fatal(err)
		return "", nil
	}

	// Scrape
	var movies []movie
	doc.Find(".lister-item").Each(func(i int, s *goquery.Selection) {
		if i < maxNumFilms {
			id, _ := s.Find(".ribbonize").Attr("data-tconst")
			rawRating, _ := s.Find(".ratings-imdb-rating").Attr("data-value")
			rating, _ := strconv.ParseFloat(rawRating, 64)
			movies = append(movies, movie{id, rating})
		}
	})

	// Marshal and send or print
	data, _ := json.Marshal(listings{movies, url})
	body := string(data)
	if local {
		fmt.Println(body)
	} else {
		err = sendSimpleMessage(body)
	}

	return "", err
}

func sendSimpleMessage(movieData string) error {
	// Env vars
	var (
		sandboxID = os.Getenv("SANDBOX_ID")
		apiKey    = os.Getenv("MAILGUN_API_KEY")
		emailAddr = os.Getenv("EMAIL_ADDRESS")
	)

	// Build payload
	vals := url.Values{}
	vals.Add("from", fmt.Sprintf("mailgun me <postmaster@sandbox%s.mailgun.org>", sandboxID))
	vals.Add("to", fmt.Sprintf("Justin <%s>", emailAddr))
	vals.Add("subject", "Popular movies released this week")
	vals.Add("text", movieData)
	body := []byte(vals.Encode())

	// Init req
	url := "https://api.mailgun.net/v3/sandbox" + sandboxID + ".mailgun.org/messages"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}

	// Add auth & build client
	req.SetBasicAuth("api", apiKey)
	req.Header.Add("content-type", "application/x-www-form-urlencoded")
	cli := &http.Client{}

	// Make request
	res, err := cli.Do(req)
	if res.StatusCode/100 != 2 {
		buf := new(bytes.Buffer)
		buf.ReadFrom(res.Body)
		err = errors.New(buf.String())
	}

	return err
}
