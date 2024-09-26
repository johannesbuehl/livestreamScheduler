package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evbuehl/livestreamScheduler/lib/googleApi"
	"github.com/jrivets/log4g"
	"google.golang.org/api/drive/v2"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/youtube/v3"
)

func (c *livestreamConfig) createBroadcast() error {
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
		return err
	} else {
		c.BroadcastID = response.Id

		return nil
	}
}

func (c livestreamConfig) addPlaylist() error {
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
			return err
		}
	}

	return nil
}

func (c livestreamConfig) setCategoryPrivacy() error {
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
		return err
	}

	return nil
}

func dlThumbnail(id string) (*http.Response, error) {
	call := googleApi.DriveService.Files.Get(id)

	if response, err := call.Download(); err != nil {
		return nil, err
	} else {
		return response, nil
	}
}

func (c livestreamConfig) setThumbnail() error {
	if thumbnail, err := dlThumbnail(c.Thumbnail); err != nil {
		return fmt.Errorf("can't download thumbnail %v", err)
	} else {
		call := googleApi.YoutubeService.Thumbnails.Set(c.BroadcastID).Media(thumbnail.Body)

		if _, err := call.Do(); err != nil {
			return fmt.Errorf("can't set thumbnail: %v", err)
		}
	}

	return nil
}

func (c livestreamConfig) moveThumbnail() error {
	parent := drive.ParentReference{
		Id: config.Thumbnails.Done,
	}

	call := googleApi.DriveService.Parents.Insert(c.Thumbnail, &parent)

	if _, err := call.Do(); err != nil {
		return err
	} else {
		return nil
	}
}

func getThumbnails() ([]*drive.File, error) {
	call := googleApi.DriveService.Files.List().
		Q(fmt.Sprintf("%q in parents and (mimeType = 'image/jpeg' or mimeType = 'image/png')", config.Thumbnails.Queue))

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

			if err := c.createBroadcast(); err != nil {
				logger.Critical("failed to create broadcast: %v", err)
			} else {
				if err := c.setCategoryPrivacy(); err != nil {
					logger.Error("failed to set category and privacy: %v", err)
				}

				if err := c.addPlaylist(); err != nil {
					logger.Error("failed to add broadcast to playlists: %v", err)
				}

				if err := c.setThumbnail(); err != nil {
					logger.Error("failed to set thumbnail: %v", err)
				}

				if err := c.moveThumbnail(); err != nil {
					logger.Critical(`failed to move thumbnail to "scheduled"-directory`)
				}
			}
		}

	} else {
		logger.Debug(fmt.Sprintf(`skipping thumbnail %q, filename doesn't match "YYYY-MM-DD.HH-MM-SS.(TITLE)?.(jpg|png)"`, thumbnail.Title))
	}
}

type mail struct {
	From    string
	Date    string
	To      string
	CC      string
	BCC     string
	Subject string
	Body    string
}

func (m mail) bytes() []byte {
	v := reflect.ValueOf(m)
	t := reflect.TypeOf(m)

	var result []string

	for ii := 0; ii < v.NumField(); ii++ {
		key := t.Field(ii).Name
		val := v.Field(ii).Interface().(string)

		// skip empty values and the body
		if key != "Body" && val != "" {
			result = append(result, fmt.Sprintf("%s: %s", key, val))
		}
	}

	return []byte(strings.Join(result, "\r\n") + "\r\n\r\n" + m.Body)
}

func sendMail() error {
	// if there is no mail-log, do nothing
	if _, err := os.Stat("mail.log"); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}

	// open the mail-log
	if f, err := os.Open("mail.log"); err != nil {
		return err
	} else {
		defer f.Close()

		if mailLog, err := io.ReadAll(f); err != nil {
			return nil
		} else {
			message := mail{
				From:    "Livestream Scheduler",
				Date:    time.Now().Format(time.RFC1123Z),
				To:      config.MailAddress,
				Subject: "Summary of livestreamScheduler",
				Body:    string(mailLog),
			}

			gmailMessage := gmail.Message{
				Raw: base64.URLEncoding.EncodeToString(message.bytes()),
			}

			call := googleApi.GmailService.Users.Messages.Send("me", &gmailMessage)

			if _, err := call.Do(); err != nil {
				return err
			} else {
				return nil
			}
		}
	}
}

var wg sync.WaitGroup

func main() {
	defer func() {
		log4g.Shutdown()

		if err := sendMail(); err != nil {
			panic(err)
		}

		if r := recover(); r != nil {
			logger.Critical(r)
			panic(r)
		}
	}()

	if thumbnails, err := getThumbnails(); err != nil {
		logger.Critical("can't get thumbnails")
	} else {
		for _, thumbnail := range thumbnails {
			wg.Add(1)

			go config.Template.handleThumbnail(thumbnail)
		}
	}

	wg.Wait()
}
