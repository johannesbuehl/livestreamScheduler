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
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/youtube/v3"
)

func (c *livestreamTemplate) createBroadcast() error {
	broadcast := youtube.LiveBroadcast{
		Snippet: &youtube.LiveBroadcastSnippet{
			Title:              "Livestream in construction",
			ScheduledStartTime: c.Date,
		},
		Status: &youtube.LiveBroadcastStatus{
			PrivacyStatus: "public",
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

func (c livestreamTemplate) addPlaylist() error {
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

func (c livestreamTemplate) setCategoryPrivacy() error {
	video := &youtube.Video{
		Id: c.BroadcastID,
		Snippet: &youtube.VideoSnippet{
			Title:       c.Title,
			CategoryId:  c.Category,
			Description: c.Description,
		},
		Status: &youtube.VideoStatus{
			PrivacyStatus: "private",
		},
	}

	call := googleApi.YoutubeService.Videos.Update([]string{"id", "snippet", "status"}, video)

	if _, err := call.Do(); err != nil {
		return err
	}

	return nil
}

func dlThumbnail(thumbnail string) (*http.Response, error) {
	call := googleApi.DriveService.Files.Get(thumbnail)

	if response, err := call.Download(); err != nil {
		return nil, err
	} else {
		return response, nil
	}
}

func (c livestreamTemplate) setThumbnail() error {
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

func (c livestreamTemplate) moveThumbnail() error {
	call := googleApi.DriveService.Files.Update(c.Thumbnail, nil).AddParents(config.Thumbnails.Done).RemoveParents(config.Thumbnails.Queue)

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
		return nil, err
	} else {
		return response.Files, nil
	}
}

var now = time.Now()

var titleParser = regexp.MustCompile(`^(?P<year>\d{4})-(?P<month>\d\d)-(?P<day>\d\d)\.(?P<hour>\d\d)-(?P<minute>\d\d)-(?P<second>\d\d)(?:\.(?P<title>.+))?\.(?:jpg|png)$`)
var subExpNames = titleParser.SubexpNames()

func (c livestreamTemplate) handleThumbnail(thumbnail *drive.File) {
	defer wg.Done()

	regexResult := titleParser.FindStringSubmatch(thumbnail.Name)

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
			logger.Info(fmt.Sprintf("Creating Livestream for %s", thumbnail.Name))

			c.Thumbnail = thumbnail.Id
			c.Date = livestreamDate.Format(time.RFC3339)

			// insert the date into the description
			strDate := fmt.Sprintf("%02d. %s %d", livestreamDate.Day(), livestreamDate.Local().Month().String(), livestreamDate.Year())
			c.Description = strings.ReplaceAll(c.Description, "DESCRIPTION_DATE", strDate)

			// if there is a title set in the thumbnail use it
			if result["title"] != "" {
				c.Title = result["title"]
			} else {
				// create the title from the template
				c.Title = strings.ReplaceAll(c.Title, "TITLE_DATE", strDate)
			}

			if err := c.createBroadcast(); err != nil {
				logger.Critical(fmt.Sprintf("failed to create broadcast: %v", err))
			} else {
				if err := c.setThumbnail(); err != nil {
					logger.Error(fmt.Sprintf("failed to set thumbnail: %v", err))
				} else if err := c.setCategoryPrivacy(); err != nil {
					logger.Error(fmt.Sprintf("failed to set category and privacy: %v", err))
				} else if err := c.addPlaylist(); err != nil {
					logger.Error(fmt.Sprintf("failed to add broadcast to playlists: %v", err))
				} else if err := c.moveThumbnail(); err != nil {
					logger.Critical(fmt.Sprintf(`failed to move thumbnail to "scheduled"-directory: %v`, err))
				}
			}
		}

	} else {
		logger.Debug(fmt.Sprintf(`skipping thumbnail %q, filename doesn't match "YYYY-MM-DD.HH-MM-SS.(TITLE)?.(jpg|png)"`, thumbnail.Name))
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
