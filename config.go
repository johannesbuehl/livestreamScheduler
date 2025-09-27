package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/evbuehl/livestreamScheduler/lib/googleApi"
	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

type livestreamTemplateJson struct {
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	Category      string   `json:"category"`
	PlaylistIDs   []string `json:"playlist_ids"`
	PrivacyStatus string   `json:"privacy_status"`
	Timezone      string   `json:"timezone"`
}

type livestreamTemplate struct {
	livestreamTemplateJson
	BroadcastID string
	Date        string
	Category    string
	PlaylistIDs []string
	Thumbnail   Thumbnail
	Timezone    *time.Location
}

type thumbnails struct {
	Queue string `json:"queue"`
	Done  string `json:"done"`
}

type configJson struct {
	LogLevel         string                 `json:"log_level"`
	MailLevel        string                 `json:"mail_level"`
	MailAddress      string                 `json:"mail_address"`
	CreationDistance string                 `json:"creation_distance"`
	RegionCode       string                 `json:"region_code"`
	Schedule         string                 `json:"schedule"`
	Thumbnails       thumbnails             `json:"thumbnails"`
	Defaults         livestreamTemplateJson `json:"defaults"`
}

type configStruct struct {
	configJson
	LogLevel         zerolog.Level `json:"log_level"`
	MailLevel        zerolog.Level `json:"mail_level"`
	CreationDistance time.Duration
	Template         livestreamTemplate
	Defaults         livestreamTemplate
}

var youtubeCategoryMap = map[string]string{}

func getCategoryMap(regionCode string) error {
	call := googleApi.YoutubeService.VideoCategories.List([]string{"snippet"}).RegionCode(regionCode)

	if response, err := call.Do(); err != nil {
		return err
	} else {
		for _, c := range response.Items {
			youtubeCategoryMap[c.Snippet.Title] = c.Id
		}

		return nil
	}
}

func loadJson() configJson {
	config := configJson{}

	jsonFile, err := os.ReadFile("config/config.json")
	if err != nil {
		logger.Panic().Msg(fmt.Sprintf("Error opening config-file: %q", err))
	}

	reader := bytes.NewReader(jsonFile)

	dec := json.NewDecoder(reader)
	dec.DisallowUnknownFields()
	err = dec.Decode(&config)
	if err != nil {
		logger.Panic().Msg(fmt.Sprintf("Error parsing config-file: %v", err))
	}

	return config
}

func loadConfigFromJson(config configJson) configStruct {
	duration, err := time.ParseDuration(config.CreationDistance)

	if err != nil {
		panic(fmt.Sprintf("can't parse CreationDistance %v", err))
	}

	if t, err := loadTemplate(config.Defaults); err != nil {
		panic(err)
	} else if logLevel, err := zerolog.ParseLevel(config.LogLevel); err != nil {
		panic(fmt.Errorf("can't parse log-level: %v", err))
	} else if mailLevel, err := zerolog.ParseLevel(config.LogLevel); err != nil {
		panic(fmt.Errorf("can't parse mail-log-level: %v", err))
	} else {
		return configStruct{
			configJson:       config,
			LogLevel:         logLevel,
			MailLevel:        mailLevel,
			CreationDistance: duration,
			Template:         t,
		}
	}
}

func loadTemplate(templateJson livestreamTemplateJson) (livestreamTemplate, error) {
	var template livestreamTemplate

	location, err := time.LoadLocation(templateJson.Timezone)

	if err != nil {
		location = time.Local
	}

	template = livestreamTemplate{
		livestreamTemplateJson: templateJson,
		Category:               youtubeCategoryMap[templateJson.Category],
		Timezone:               location,
		PlaylistIDs:            templateJson.PlaylistIDs,
	}

	return template, nil
}

var config configStruct

var logger zerolog.Logger

type specificLevelWriter struct {
	io.Writer
	Level zerolog.Level
}

func (w specificLevelWriter) WriteLevel(l zerolog.Level, p []byte) (int, error) {
	if l >= w.Level {
		return w.Write(p)
	} else {
		return len(p), nil
	}
}

func loadConfig() {
	configJson := loadJson()

	// get the youtube category map
	getCategoryMap(configJson.RegionCode)

	// now parse the configJson
	config = loadConfigFromJson(configJson)

	// try to set the log-level
	zerolog.SetGlobalLevel(config.LogLevel)
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	// create the console output
	outputConsole := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.DateTime,
		FormatLevel: func(i interface{}) string {
			return strings.ToUpper(fmt.Sprintf("| %-6s|", i))
		},
		FormatFieldName: func(i interface{}) string {
			return fmt.Sprintf("%s", i)
		},
	}

	// create the logfile output
	outputLog := &lumberjack.Logger{
		Filename:  "logs/livestream-scheduler.log",
		MaxAge:    7,
		LocalTime: true,
	}

	// create the mail output
	outputMail := outputConsole
	outputMail.NoColor = true
	outputMail.Out = &lumberjack.Logger{
		Filename: "mail.log",
	}

	// create a multi-output-writer
	multi := zerolog.MultiLevelWriter(
		specificLevelWriter{
			Writer: outputConsole,
			Level:  config.LogLevel,
		},
		specificLevelWriter{
			Writer: outputLog,
			Level:  config.LogLevel,
		},
		specificLevelWriter{
			Writer: outputMail,
			Level:  config.MailLevel,
		},
	)

	// create a logger-instance
	logger = zerolog.New(multi).With().Timestamp().Logger()
}

func init() {
	loadConfig()
}
