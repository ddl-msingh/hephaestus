package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"
)

type Controller struct {
	Logging   Logging   `json:"logging" yaml:"logging"`
	Manager   Manager   `json:"manager" yaml:"manager"`
	Buildkit  Buildkit  `json:"buildkit" yaml:"buildkit"`
	Messaging Messaging `json:"messaging" yaml:"messaging"`

	ImageBuildMaxConcurrency int `json:"imageBuildMaxConcurrency" yaml:"imageBuildMaxConcurrency"`
}

func (c Controller) Validate() error {
	var errs []string

	if c.Manager.HealthProbeAddr == "" {
		errs = append(errs, "manager.healthProbeAddr cannot be blank")
	}
	if c.Manager.MetricsAddr == "" {
		errs = append(errs, "manager.metricsAddr cannot be blank")
	}
	if err := validatePort(c.Manager.WebhookPort); err != nil {
		errs = append(errs, fmt.Sprintf("manager.webhookPort is invalid: %s", err.Error()))
	}

	if c.Buildkit.Labels == nil {
		errs = append(errs, "buildkit.labels cannot be nil")
	}
	if c.Buildkit.Namespace == "" {
		errs = append(errs, "buildkit.namespace cannot be blank")
	}
	if err := validatePort(int(c.Buildkit.DaemonPort)); err != nil {
		errs = append(errs, fmt.Sprintf("buildkit.daemonPort is invalid: %s", err.Error()))
	}

	if len(errs) != 0 {
		return fmt.Errorf("config is invalid: %s", strings.Join(errs, ", "))
	}

	return nil
}

type Logging struct {
	Development     bool   `json:"development" yaml:"development"`
	Encoder         string `json:"encoder" yaml:"encoder"`
	LogLevel        string `json:"logLevel" yaml:"logLevel"`
	StacktraceLevel string `json:"stacktraceLevel" yaml:"stacktraceLevel"`
}

type Manager struct {
	HealthProbeAddr      string   `json:"healthProbeAddr" yaml:"healthProbeAddr"`
	MetricsAddr          string   `json:"metricsAddr" yaml:"metricsAddr"`
	WebhookPort          int      `json:"webhookPort" yaml:"webhookPort"`
	WatchNamespaces      []string `json:"watchNamespaces" yaml:"watchNamespaces"`
	EnableLeaderElection bool     `json:"enableLeaderElection" yaml:"enableLeaderElection"`
}

type Buildkit struct {
	Labels     map[string]string `json:"labels" yaml:"labels"`
	Namespace  string            `json:"namespace" yaml:"namespace"`
	DaemonPort int32             `json:"daemonPort" yaml:"daemonPort"`

	CACertPath string `json:"caCertPath" yaml:"caCertPath"`
	CertPath   string `json:"certPath" yaml:"certPath"`
	KeyPath    string `json:"keyPath" yaml:"keyPath"`
}

type Messaging struct {
	Enabled bool            `json:"enabled" yaml:"enabled"`
	AMQP    *AMQPMessaging  `json:"amqp" yaml:"amqp"`
	Kafka   *KafkaMessaging `json:"kafka" yaml:"kafka"`
}

type AMQPMessaging struct {
	URL      string `json:"url" yaml:"url"`
	Exchange string `json:"exchange" yaml:"exchange"`
	Queue    string `json:"queue" yaml:"queue"`
}

type KafkaMessaging struct {
	Servers   []string `json:"servers" yaml:"servers"`
	Topic     string   `json:"topic" yaml:"topic"`
	Partition string   `json:"partition" yaml:"partition"`
}

func GenerateDefaults() Controller {
	return Controller{
		Manager: Manager{
			HealthProbeAddr:      ":8081",
			MetricsAddr:          ":8080",
			WebhookPort:          9443,
			WatchNamespaces:      nil,
			EnableLeaderElection: false,
		},
		Buildkit: Buildkit{
			Labels: map[string]string{
				"app": "buildkit",
			},
			Namespace:  "default",
			DaemonPort: 1234,
		},
		Logging: Logging{
			Development:     false,
			Encoder:         "",
			LogLevel:        "",
			StacktraceLevel: "",
		},
		ImageBuildMaxConcurrency: 5,
	}
}

func LoadFromFile(filename string) (Controller, error) {
	bs, err := os.ReadFile(filename)
	if err != nil {
		return Controller{}, err
	}

	var cfg Controller
	switch ext := filepath.Ext(filename); ext {
	case ".yaml", ".yml":
		err = yaml.Unmarshal(bs, &cfg)
	case ".json":
		err = json.Unmarshal(bs, &cfg)
	default:
		return Controller{}, fmt.Errorf("file extensions %q is not allowed", ext)
	}

	return cfg, err
}

func validatePort(port int) error {
	if port < 1024 || port > 65535 {
		return fmt.Errorf("port %d must be between 1024 and 65535", port)
	}

	return nil
}
