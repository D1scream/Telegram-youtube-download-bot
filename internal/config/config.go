package config

import (
	"strings"

	"github.com/caarlos0/env/v11"

	"telegram-bot/internal/entities"
)

type Config struct {
	BotToken                 string  `env:"BOT_TOKEN,required"`
	Port                     string  `env:"PORT" envDefault:"9000"`
	LLMAPIKey                string  `env:"LLM_API"`
	LLMSystemPromptPath      string  `env:"LLM_SYSTEM_PROMPT_PATH" envDefault:"prompts/image_analysis_system.txt"`
	OpenSearchURL            string  `env:"OPENSEARCH_URL"`
	OpenSearchIndex          string  `env:"OPENSEARCH_INDEX" envDefault:"uk_rf"`
	OpenSearchSearchPipeline string  `env:"OPENSEARCH_SEARCH_PIPELINE" envDefault:"uk_rf-hybrid"`
	EmbeddingsURL            string  `env:"EMBEDDINGS_URL" envDefault:"http://localhost:8080"`
	SearchKNNK               int     `env:"SEARCH_KNN_K" envDefault:"20"`
	SearchMinScore           float64 `env:"SEARCH_MIN_SCORE" envDefault:"0.55"`
	SlskdURL                 string  `env:"SLSKD_URL"`
	SlskdAPIKey              string  `env:"SLSKD_API_KEY"`
	SlskdSearchFileLimit     int     `env:"SLSKD_SEARCH_FILE_LIMIT" envDefault:"50"`
	SlskdSearchDisplayLimit  int     `env:"SLSKD_SEARCH_DISPLAY_LIMIT" envDefault:"10"`
	SlskdAllowedFormats      string  `env:"SLSKD_ALLOWED_FORMATS" envDefault:"mp3,flac,ogg,wav,m4a,aac"`
	SlskdWebhookSecret       string  `env:"SLSKD_WEBHOOK_SECRET"`
	SlskdWebhookCallbackURL  string  `env:"SLSKD_WEBHOOK_CALLBACK_URL" envDefault:"http://host.docker.internal:9000/webhooks/slskd"`
	SlskdDownloadsDir        string  `env:"SLSKD_DOWNLOADS_DIR" envDefault:"docker/slskd/data/downloads"`
	SlskdMusicDir            string  `env:"SLSKD_MUSIC_DIR" envDefault:"music"`
	UploadedMusicDir         string  `env:"UPLOADED_MUSIC_DIR" envDefault:"uploaded_music"`
}

func LoadConfig() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) MusicAllowedFormats() []entities.AudioFormat {
	formats := make([]entities.AudioFormat, 0, 3)
	for _, part := range strings.Split(c.SlskdAllowedFormats, ",") {
		part = strings.TrimPrefix(strings.TrimSpace(part), ".")
		if part == "" {
			continue
		}
		formats = append(formats, entities.AudioFormat("."+strings.ToLower(part)))
	}
	return formats
}
