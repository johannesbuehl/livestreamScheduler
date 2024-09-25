package main

import (
	"bytes"
	"os"
	"time"

	"github.com/evbuehl/livestreamScheduler/lib/googleApi"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/yaml.v3"
)

type livestreamConfigYaml struct {
	Title       string   `yaml:"Title"`
	Category    string   `yaml:"Category"`
	PlaylistIDs []string `yaml:"PlaylistIDs"`
}

type livestreamConfig struct {
	livestreamConfigYaml
	BroadcastID string
	Date        string
	Category    string
	PlaylistIDs []string
	Thumbnail   string
}

type configYaml struct {
	LogLevel         string               `yaml:"LogLevel"`
	CreationDistance string               `yaml:"CreationDistance"`
	RegionCode       string               `yaml:"RegionCode"`
	Template         livestreamConfigYaml `yaml:"Template"`
}

type configStruct struct {
	configYaml
	CreationDistance time.Duration
	Template         livestreamConfig
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
		logger.Sugar().Errorf("Error opening config-file: %q", err)
	}

	reader := bytes.NewReader(yamlFile)

	dec := yaml.NewDecoder(reader)
	dec.KnownFields(true)
	err = dec.Decode(&config)
	if err != nil {
		logger.Sugar().Errorf("Error parsing config-file: %q", err.Error())
		os.Exit(1)
	}

	return config
}

func loadConfig(config configYaml) configStruct {
	duration, err := time.ParseDuration(config.CreationDistance)

	if err != nil {
		logger.Sugar().Errorf("can't parse CreationDistance %v", err)
		os.Exit(1)
	}

	return configStruct{
		configYaml:       config,
		CreationDistance: duration,
		Template: livestreamConfig{
			livestreamConfigYaml: config.Template,
			Category:             youtubeCategoryMap[config.Template.Category],
		},
	}
}

var config configStruct

func init() {
	configYaml := loadYaml()

	// initialize the logger
	stdout := zapcore.AddSync(os.Stdout)

	file := zapcore.AddSync(&lumberjack.Logger{
		Filename: "logs/server.log",
		MaxSize:  10,
	})

	level, err := zapcore.ParseLevel(configYaml.LogLevel)

	if err != nil {
		level = zapcore.InfoLevel
	}

	productionConfig := zap.NewProductionEncoderConfig()
	productionConfig.TimeKey = "timestamp"
	productionConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	developmentConfig := zap.NewDevelopmentEncoderConfig()
	developmentConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder

	consoleEncoder := zapcore.NewConsoleEncoder(developmentConfig)
	fileEncoder := zapcore.NewJSONEncoder(productionConfig)

	core := zapcore.NewTee(
		zapcore.NewCore(consoleEncoder, stdout, level),
		zapcore.NewCore(fileEncoder, file, level),
	)

	zap.ReplaceGlobals(zap.Must(zap.NewProduction()))

	logger = *zap.New(core, zap.AddCaller())

	// get the youtube category map
	getCategoryMap()

	// now parse the configYaml
	config = loadConfig(configYaml)
}
