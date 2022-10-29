package joi

import (
	"encoding/json"
	tele "gopkg.in/telebot.v3"
	"os"
)

const (
	DefaultTemporaryFilesDirectory = "."
	DefaultRedisPrefix             = "joi"
	DefaultRedisAddress            = "localhost:6379"
	DefaultParseMode               = tele.ModeMarkdownV2
	DefaultDefaultPostText         = ""
)

type Config struct {
	Token            string   `json:"token"`
	AdminList        []int64  `json:"admin-list"`
	DefaultPostTimes []string `json:"default-post-times"`
	ChannelId        int64    `json:"channel-id"`
	CommentsId       int64    `json:"comments-id"`

	TemporaryFilesDirectory string `json:"temporary-files-directory,omitempty"`
	RedisPrefix             string `json:"redis-prefix,omitempty"`
	RedisAddress            string `json:"redis-address,omitempty"`
	RedisDatabaseNumber     int    `json:"redis-database-number,omitempty"`

	DefaultPostText       string `json:"default-post-text,omitempty"`
	ParseMode             string `json:"parse-mode,omitempty"`
	DisableWebPagePreview bool   `json:"disable-web-page-preview,omitempty"`
	DisableNotification   bool   `json:"disable-notification,omitempty"`
}

func (cfg Config) FillDefaults() Config {
	if cfg.TemporaryFilesDirectory == "" {
		cfg.TemporaryFilesDirectory = DefaultTemporaryFilesDirectory
	}
	if cfg.RedisPrefix == "" {
		cfg.RedisPrefix = DefaultRedisPrefix
	}
	if cfg.RedisAddress == "" {
		cfg.RedisAddress = DefaultRedisAddress
	}
	if cfg.ParseMode == "" {
		cfg.ParseMode = DefaultParseMode
	}
	if cfg.DefaultPostText == "" {
		cfg.DefaultPostText = DefaultDefaultPostText
	}
	return cfg
}

func LoadConfig(filename string) (cfg Config, err error) {
	buff, err := os.ReadFile(filename)
	if err != nil {
		return
	}
	err = json.Unmarshal(buff, &cfg)
	if err != nil {
		return
	}
	return cfg, nil
}

func (cfg Config) DumpConfig(filename string) error {
	buffer, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(filename, buffer, os.ModePerm)
}
