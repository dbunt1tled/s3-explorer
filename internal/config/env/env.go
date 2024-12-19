package env

import (
	"log"
	"os"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	S3 S3
}

type S3 struct {
	AccessKey string `env:"AWS_ACCESS_KEY_ID" env-required:"true"`
	SecretKey string `env:"AWS_SECRET_ACCESS_KEY" env-required:"true"`
	Region    string `env:"AWS_DEFAULT_REGION" env-required:"true"`
}

func MustLoadConfig() *Config {
	var cfg Config
	configPath := ".env"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		err = cleanenv.ReadEnv(&cfg)
		if err != nil {
			log.Fatalf("Error load config enviroment: %s", err)
		}
	} else {
		err = cleanenv.ReadConfig(configPath, &cfg)
		if err != nil {
			log.Fatalf("Error load config file enviroment: %s", err)
		}
	}

	return &cfg
}

var instance *Config //nolint:gochecknoglobals // singleton

func GetConfigInstance() *Config {
	if instance == nil {
		instance = MustLoadConfig()
	}
	return instance
}
