package main

import (
	"bytes"
	"encoding/json"
	"os"

	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/aws/aws-lambda-go/lambda"
)

type (
	movie struct {
		ID       string   `json:"id"`
		Title    string   `json:"title"`
		Year     string   `json:"year"`
		Plot     string   `json:"plot"`
		Score    int      `json:"metascore"`
		Director string   `json:"director"`
		Actors   []string `json:"actors"`
	}

	listings struct {
		New []movie `json:"new_movies"`
		Old []movie `json:"old_movies"`
	}
)

func main() {
	lambda.Start(handleRequest)
}

func handleRequest() (string, error) {
	// Make IMDB request
	url := "http://www.imdb.com/movies-in-theaters/"
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

	// Declare vars
	var (
		newSel, oldSel       *goquery.Selection
		newMovies, oldMovies []movie
	)

	// Find movie lists, then find the overview-tops
	doc.Find(".list").Each(func(i int, s *goquery.Selection) {
		if i == 0 {
			newSel = s.Find(".overview-top")
		}
		if i == 1 {
			oldSel = s.Find(".overview-top")
		}
	})

	// Fill new movies
	newSel.Each(func(i int, new *goquery.Selection) {
		title, year := getTitleAndYear(new)
		m := movie{
			ID:       getID(new),
			Title:    title,
			Year:     year,
			Plot:     getPlot(new),
			Director: getDirector(new),
			Actors:   getActors(new),
		}
		newMovies = append(newMovies, m)
	})

	// Fill old movies
	oldSel.Each(func(i int, old *goquery.Selection) {
		title, year := getTitleAndYear(old)
		m := movie{
			ID:       getID(old),
			Title:    title,
			Year:     year,
			Plot:     getPlot(old),
			Director: getDirector(old),
			Actors:   getActors(old),
		}
		oldMovies = append(oldMovies, m)
	})

	// Build json & send
	data, _ := json.Marshal(listings{newMovies, oldMovies})
	body := strings.Replace(string(data), "null", "[]", 1) // https://github.com/golang/go/issues/31811
	err = sendSimpleMessage(body)
	return "", err
}

func getID(s *goquery.Selection) (id string) {
	href, ok := s.Find("a").First().Attr("href")
	if ok {
		id = strings.Split(href, "/")[2]
	}
	return
}

func getTitleAndYear(s *goquery.Selection) (title string, year string) {
	raw := s.Find("a").First().Text()
	split := strings.Split(raw, " (")
	title = strings.TrimSpace(split[0])
	year = strings.TrimRight(split[1], ")")
	return
}

func getPlot(s *goquery.Selection) string {
	plot := s.Find(".outline").Text()
	plot = strings.Replace(plot, "\n", "", 1)
	return strings.TrimSpace(plot)
}

func getScore(s *goquery.Selection) (score int) {
	el := s.Find(".metascore")
	if el != nil {
		score, _ = strconv.Atoi(el.Text())
	}
	return
}

func getDirector(s *goquery.Selection) string {
	return s.Find(".txt-block").First().Find("a").Text()
}

func getActors(s *goquery.Selection) []string {
	var cast []string
	castTextBlock := s.Find(".txt-block").Last().Find("a")

	castTextBlock.Each(func(i int, sel *goquery.Selection) {
		cast = append(cast, sel.Text())
	})

	return cast
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
	vals.Add("subject", "Movies released this week")
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
