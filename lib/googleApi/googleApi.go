package googleApi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v2"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

var YoutubeService *youtube.Service
var DriveService *drive.Service
var GmailService *gmail.Service

func getClient(config *oauth2.Config) (*http.Client, error) {
	tokenFile := "token.json"

	token, err := getTokenFromFile(tokenFile)

	if err != nil {
		token, err = getTokenFromWeb(config)

		if err != nil {
			return nil, fmt.Errorf("can't get token from web: %v", err)
		}

		if err := saveTokenToFile(tokenFile, token); err != nil {
			return nil, err
		}
	}

	// if the token is invalid, try to refresh it
	if !token.Valid() {
		newToken, err := refreshToken(config, token)

		if err != nil {
			return nil, fmt.Errorf("can't refresh token: %v", err)
		}

		token = newToken

		// save the refreshed token to file
		if err := saveTokenToFile(tokenFile, token); err != nil {
			return nil, fmt.Errorf("can't save refreshed token to file: %v", err)
		}
	}

	return config.Client(context.Background(), token), nil
}

func getTokenFromWeb(config *oauth2.Config) (*oauth2.Token, error) {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		return nil, fmt.Errorf("unable to read authorization code: %v", err)
	}

	if tok, err := config.Exchange(context.TODO(), authCode); err != nil {
		return nil, fmt.Errorf("unable to retrieve token from web: %v", err)
	} else {
		return tok, nil
	}
}

func refreshToken(config *oauth2.Config, token *oauth2.Token) (*oauth2.Token, error) {
	// check if the token has a refresh token
	if !token.Valid() && token.RefreshToken != "" {
		tokenSource := config.TokenSource(context.Background(), token)

		// automatically refreh the token

		if newToken, err := tokenSource.Token(); err != nil {
			return nil, fmt.Errorf("unable to refresh token: %v", err)
		} else {
			return newToken, nil
		}
	} else {
		return nil, fmt.Errorf("token is invalid and no refresh token is available")
	}
}

func saveTokenToFile(file string, token *oauth2.Token) error {
	f, err := os.Create(file)

	if err != nil {
		return err
	}

	defer f.Close()

	json.NewEncoder(f).Encode(token)

	return nil
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
		if config, err := google.ConfigFromJSON(b, youtube.YoutubeScope, drive.DriveScope, gmail.GmailSendScope); err != nil {
		} else {
			if client, err := getClient(config); err != nil {
				panic(err)
			} else {
				if service, err := youtube.NewService(ctx, option.WithHTTPClient(client)); err == nil {
					YoutubeService = service
				}

				if service, err := drive.NewService(ctx, option.WithHTTPClient(client)); err == nil {
					DriveService = service
				}

				if service, err := gmail.NewService(ctx, option.WithHTTPClient(client)); err == nil {
					GmailService = service
				}
			}
		}
	}
}
