package main

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evbuehl/livestreamScheduler/lib/googleApi"
	"go.uber.org/zap"
	"google.golang.org/api/drive/v2"
	"google.golang.org/api/youtube/v3"
)

var logger zap.Logger

func (c *livestreamConfig) createBroadcast() {
	broadcast := youtube.LiveBroadcast{
		Snippet: &youtube.LiveBroadcastSnippet{
			Title:              "Livestream in construction",
			ScheduledStartTime: c.Date,
		},
		Status: &youtube.LiveBroadcastStatus{
			PrivacyStatus: "private",
		},
		ContentDetails: &youtube.LiveBroadcastContentDetails{
			EnableAutoStart: true,
			EnableAutoStop:  true,
		},
	}

	call := googleApi.YoutubeService.LiveBroadcasts.Insert([]string{"snippet", "status", "contentDetails"}, &broadcast)

	if response, err := call.Do(); err != nil {
		logger.Fatal(fmt.Sprintf("Error creating broadcast: %v", err))
	} else {
		c.BroadcastID = response.Id
	}
}

func (c livestreamConfig) addPlaylist() {
	for _, playlistID := range c.PlaylistIDs {
		playlistItem := &youtube.PlaylistItem{
			Snippet: &youtube.PlaylistItemSnippet{
				PlaylistId: playlistID,
				ResourceId: &youtube.ResourceId{
					Kind:    "youtube#video",
					VideoId: c.BroadcastID,
				},
			},
		}

		call := googleApi.YoutubeService.PlaylistItems.Insert([]string{"snippet"}, playlistItem)

		if _, err := call.Do(); err != nil {
			logger.Error(fmt.Sprintf("error adding playlist %q: %v", playlistID, err))
		}
	}
}

func (c livestreamConfig) addCategory() {
	video := &youtube.Video{
		Id: c.BroadcastID,
		Snippet: &youtube.VideoSnippet{
			Title:      c.Title,
			CategoryId: c.Category,
		},
		Status: &youtube.VideoStatus{
			PrivacyStatus: "unlisted",
		},
	}

	call := googleApi.YoutubeService.Videos.Update([]string{"id", "snippet", "status"}, video)

	if _, err := call.Do(); err != nil {
		logger.Error(fmt.Sprintf("error setting category and privacy-status: %v", err))
	}
}

func dlThumbnail(id string) (*http.Response, error) {
	call := googleApi.DriveService.Files.Get(id)

	if response, err := call.Download(); err != nil {
		return nil, err
	} else {
		return response, nil
	}
}

func (c livestreamConfig) setThumbnail() {
	if thumbnail, err := dlThumbnail(c.Thumbnail); err != nil {
		logger.Error(fmt.Sprintf("can't download thumbnail %v", err))
	} else {
		call := googleApi.YoutubeService.Thumbnails.Set(c.BroadcastID).Media(thumbnail.Body)

		if response, err := call.Do(); err != nil {
			logger.Error(fmt.Sprintf("can't set thumbnail: %v", err))
		} else {
			logger.Debug(response.EventId)
		}
	}
}

func getThumbnails() ([]*drive.File, error) {
	// q: mimeType = 'application/vnd.google-apps.folder'

	call := googleApi.DriveService.Files.List().
		Q("'110N0zGP8Pqwlf_4zz_RdSr9J6JKgTpUM' in parents and (mimeType = 'image/jpeg' or mimeType = 'image/png')")

	if response, err := call.Do(); err != nil {
		return nil, nil
	} else {
		return response.Items, nil
	}
}

var now = time.Now()

var titleParser = regexp.MustCompile(`^(?P<year>\d{4})-(?P<month>\d\d)-(?P<day>\d\d)\.(?P<hour>\d\d)-(?P<minute>\d\d)-(?P<second>\d\d)(?:\.(?P<title>.+))?\.(?:jpg|png)$`)
var subExpNames = titleParser.SubexpNames()

func (c livestreamConfig) handleThumbnail(thumbnail *drive.File) {
	defer wg.Done()

	regexResult := titleParser.FindStringSubmatch(thumbnail.Title)

	if len(regexResult) > 0 {
		result := make(map[string]string)

		for ii, name := range subExpNames {
			if ii != 0 && name != "" {
				result[name] = regexResult[ii]
			}
		}

		d := map[string]int{}
		fields := []string{"year", "month", "day", "hour", "minute", "second"}

		for _, field := range fields {
			if number, err := strconv.Atoi(result[field]); err != nil {
				return
			} else {
				d[field] = number
			}
		}

		livestreamDate := time.Date(d["year"], time.Month(d["month"]), d["day"], d["hour"], d["minute"], d["second"], 0, time.Local)

		timeUntilLive := livestreamDate.Sub(now)

		if timeUntilLive > 0 && timeUntilLive < config.CreationDistance {
			logger.Info(fmt.Sprintf("Creating Livestream for %s", thumbnail.Title))

			c.Thumbnail = thumbnail.Id
			c.Date = livestreamDate.Format(time.RFC3339)

			// if there is a title set in the thumbnail use it
			if result["title"] != "" {
				c.Title = result["title"]
			} else {
				// create the title from the template
				c.Title = strings.ReplaceAll(c.Title, "TITLE_DATE", fmt.Sprintf("%02d. %s %d", livestreamDate.Day(), livestreamDate.Local().Month().String(), livestreamDate.Year()))
			}

			c.createBroadcast()
			c.addCategory()
			c.addPlaylist()
			c.setThumbnail()
		}

	} else {
		logger.Info(fmt.Sprintf(`skipping thumbnail %q, filename doesn't match "YYYY-MM-DD.HH-MM-SS.(TITLE)?.(jpg|png)"`, thumbnail.Title))
	}
}

var wg sync.WaitGroup

func main() {
	defer logger.Sync()

	if thumbnails, err := getThumbnails(); err != nil {
		logger.Fatal("can't get thumbnails")
	} else {
		for _, thumbnail := range thumbnails {
			wg.Add(1)

			go config.Template.handleThumbnail(thumbnail)
		}
	}

	wg.Wait()

	// config := livestreamConfig{
	// 	title:       "test-stream",
	// 	date:        "2024-09-30T12:00:00Z",
	// 	category:    "22",
	// 	playlistIDs: []string{"PLGdIyIZ8SJQq88MQl3gC0Afp2yFcfcpeQ"},
	// }

	// config.createBroadcast()
	// config.addCategory()
	// config.addPlaylist()
}
