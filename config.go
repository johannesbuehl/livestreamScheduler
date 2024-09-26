package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/evbuehl/livestreamScheduler/lib/googleApi"
	"github.com/xtfly/log4g"
	"github.com/xtfly/log4g/api"
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
		logger.Critical(fmt.Sprintf("Error opening config-file: %q", err))
		panic(err)
	}

	reader := bytes.NewReader(yamlFile)

	dec := yaml.NewDecoder(reader)
	dec.KnownFields(true)
	err = dec.Decode(&config)
	if err != nil {
		logger.Critical(fmt.Sprintf("Error parsing config-file: %v", err))
		panic(err)
	}

	return config
}

func loadConfig(config configYaml) configStruct {
	duration, err := time.ParseDuration(config.CreationDistance)

	if err != nil {
		logger.Critical(fmt.Sprintf("can't parse CreationDistance %v", err))

		panic(err)
	}

	if t, err := loadTemplate(); err != nil {
		panic(err)
	} else {
		return configStruct{
			configYaml:       config,
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

var logger api.Logger

func init() {
	configYaml := loadYaml()

	// initialize the logger
	cfg := &api.Config{
		Loggers: []api.CfgLogger{
			{Name: "root", Level: configYaml.LogLevel, OutputNames: []string{"console", "log", "mail"}},
		},
		Formats: []api.CfgFormat{
			{"type": "text", "name": "std", "layout": "[%{time}] [%{level}] %{msg}\n"},
		},
		Outputs: []api.CfgOutput{
			{"type": "console", "name": "console", "format": "std"},
			{"type": "time_rolling_file", "name": "log", "file": "logs/livestreamScheduler.log", "pattern": "2006-01-02", "backups": "7", "format": "std"},
			{"type": "size_rolling_file", "name": "mail", "file": "mail.log", "format": "std", "threshold": configYaml.MailLevel},
		},
	}

	if err := log4g.GetManager().SetConfig(cfg); err != nil {
		panic(err)
	}

	logger = log4g.GetLogger("default")

	// get the youtube category map
	getCategoryMap()

	// now parse the configYaml
	config = loadConfig(configYaml)
}
