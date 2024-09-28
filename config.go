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
	"gopkg.in/yaml.v3"
)

type livestreamTemplateJson struct {
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	Category      string   `json:"category"`
	PlaylistIDs   []string `json:"playlist_ids"`
	PrivacyStatus string   `json:"privacy_status"`
}

type livestreamTemplate struct {
	livestreamTemplateJson
	BroadcastID string
	Date        string
	Category    string
	PlaylistIDs []string
	Thumbnail   string
}

type thumbnails struct {
	Queue string `yaml:"Queue"`
	Done  string `yaml:"Done"`
}

type configYaml struct {
	LogLevel         string     `yaml:"LogLevel"`
	MailLevel        string     `yaml:"MailLevel"`
	MailAddress      string     `yaml:"MailAddress"`
	CreationDistance string     `yaml:"CreationDistance"`
	RegionCode       string     `yaml:"RegionCode"`
	Thumbnails       thumbnails `yaml:"Thumbnails"`
}

type configStruct struct {
	configYaml
	LogLevel         zerolog.Level `yaml:"LogLevel"`
	MailLevel        zerolog.Level `yaml:"MailLevel"`
	CreationDistance time.Duration
	Template         livestreamTemplate
}

var youtubeCategoryMap = map[string]string{}

func getCategoryMap() error {
	call := googleApi.YoutubeService.VideoCategories.List([]string{"snippet"}).RegionCode(config.RegionCode)

	if response, err := call.Do(); err != nil {
		return err
	} else {
		for _, c := range response.Items {
			youtubeCategoryMap[c.Snippet.Title] = c.Id
		}

		return nil
	}
}

func loadYaml() configYaml {
	config := configYaml{}

	yamlFile, err := os.ReadFile("config.yaml")
	if err != nil {
		logger.Panic().Msg(fmt.Sprintf("Error opening config-file: %q", err))
	}

	reader := bytes.NewReader(yamlFile)

	dec := yaml.NewDecoder(reader)
	dec.KnownFields(true)
	err = dec.Decode(&config)
	if err != nil {
		logger.Panic().Msg(fmt.Sprintf("Error parsing config-file: %v", err))
	}

	return config
}

func loadConfig(config configYaml) configStruct {
	duration, err := time.ParseDuration(config.CreationDistance)

	if err != nil {
		panic(fmt.Sprintf("can't parse CreationDistance %v", err))
	}

	if t, err := loadTemplate(); err != nil {
		panic(err)
	} else if logLevel, err := zerolog.ParseLevel(config.LogLevel); err != nil {
		panic(fmt.Errorf("can't parse log-level: %v", err))
	} else if mailLevel, err := zerolog.ParseLevel(config.LogLevel); err != nil {
		panic(fmt.Errorf("can't parse mail-log-level: %v", err))
	} else {
		return configStruct{
			configYaml:       config,
			LogLevel:         logLevel,
			MailLevel:        mailLevel,
			CreationDistance: duration,
			Template:         t,
		}
	}

}

func loadTemplate() (livestreamTemplate, error) {
	var template livestreamTemplate
	templateJson := livestreamTemplateJson{}

	call := googleApi.DriveService.Files.List().
		Q("name = 'defaults.json'")
		// Q(fmt.Sprintf("%q in parents and name = \"defaults.json\"", config.Thumbnails.Queue))

	if response, err := call.Do(); err != nil {
		return template, err
	} else if len(response.Files) == 0 {
		return template, fmt.Errorf(`can't find "defaults.json"`)
	} else {
		// download the defaults-file

		call := googleApi.DriveService.Files.Get(response.Files[0].Id)

		if response, err := call.Download(); err != nil {
			return template, fmt.Errorf(`can't download "defaults.json": %v`, err)
		} else {
			if err = json.NewDecoder(response.Body).Decode(&templateJson); err != nil {
				return template, nil
			} else {
				template = livestreamTemplate{
					livestreamTemplateJson: templateJson,
					Category:               youtubeCategoryMap[templateJson.Category],
				}
			}
		}
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

func init() {
	configYaml := loadYaml()

	// get the youtube category map
	getCategoryMap()

	// now parse the configYaml
	config = loadConfig(configYaml)

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
		Filename:  "logs/livestreamScheduler.log",
		MaxAge:    7,
		LocalTime: true,
	}

	// create the mail output
	outputMail := outputConsole
	outputMail.NoColor = true
	outputMail.Out = &lumberjack.Logger{
		Filename: "Mail.log",
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
