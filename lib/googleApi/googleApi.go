package googleApi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v2"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

var YoutubeService *youtube.Service
var DriveService *drive.Service

func getClient(config *oauth2.Config) *http.Client {
	tokenFile := "token.json"

	token, err := getTokenFromFile(tokenFile)

	if err != nil {
		token = getTokenFromWeb(config)

		saveTokenToFile(tokenFile, token)
	}

	return config.Client(context.Background(), token)
}

func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

func saveTokenToFile(file string, token *oauth2.Token) {
	f, err := os.Create(file)

	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}

	defer f.Close()

	json.NewEncoder(f).Encode(token)
}

func getTokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)

	if err != nil {
		return nil, err
	}

	defer f.Close()

	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)

	return tok, err
}

func init() {
	ctx := context.Background()

	// initialize the youtube-api
	// load credentials.json
	if b, err := os.ReadFile("credentials.json"); err != nil {
	} else {
		if config, err := google.ConfigFromJSON(b, youtube.YoutubeScope, drive.DriveReadonlyScope); err != nil {
		} else {
			client := getClient(config)

			if service, err := youtube.NewService(ctx, option.WithHTTPClient(client)); err == nil {
				YoutubeService = service
			}

			if service, err := drive.NewService(ctx, option.WithHTTPClient(client)); err == nil {
				DriveService = service
			}
		}
	}
}
