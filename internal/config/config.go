package config

import (
	"github.com/caarlos0/env/v11"
)

type Config struct {
	BotToken                 string  `env:"BOT_TOKEN,required"`
	LLMAPIKey                string  `env:"LLM_API"`
	LLMSystemPromptPath      string  `env:"LLM_SYSTEM_PROMPT_PATH" envDefault:"prompts/image_analysis_system.txt"`
	OpenSearchURL            string  `env:"OPENSEARCH_URL"`
	OpenSearchIndex          string  `env:"OPENSEARCH_INDEX" envDefault:"uk_rf"`
	OpenSearchSearchPipeline string  `env:"OPENSEARCH_SEARCH_PIPELINE" envDefault:"uk_rf-hybrid"`
	EmbeddingsURL            string  `env:"EMBEDDINGS_URL" envDefault:"http://localhost:8080"`
	SearchKNNK               int     `env:"SEARCH_KNN_K" envDefault:"20"`
	SearchMinScore           float64 `env:"SEARCH_MIN_SCORE" envDefault:"0.55"`
	YtdlpEnabled             bool    `env:"YT_DLP_ENABLED" envDefault:"false"`
	YtdlpPath                string  `env:"YT_DLP_PATH" envDefault:"yt-dlp"`
	YtdlpDownloadDir         string  `env:"YT_DLP_DOWNLOAD_DIR" envDefault:"yt_downloads"`
	YtdlpCookiesFile         string  `env:"YT_DLP_COOKIES_FILE"`
	YtdlpCookiesFromBrowser  string  `env:"YT_DLP_COOKIES_FROM_BROWSER"`
}

func LoadConfig() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
