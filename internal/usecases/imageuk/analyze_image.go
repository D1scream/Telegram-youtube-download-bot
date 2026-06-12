package imageuk

import (
	"context"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot/models"

	"telegram-bot/internal/entities"
)

type PhotoFetcher interface {
	FetchPhoto(ctx context.Context, msg *models.Message) ([]byte, string, error)
}

type ImageAnalyzer interface {
	AnalyzeImage(ctx context.Context, data []byte, mime string) (entities.ImageAnalysis, error)
}

type ArticleSearcher interface {
	Search(ctx context.Context, query string, limit int) ([]entities.ScoredArticle, error)
}

type Messenger interface {
	ReplyToChat(ctx context.Context, chatID int64, messageID int, text string) (int, error)
}

type AnalyzeImageService struct {
	fetcher        PhotoFetcher
	analyzer       ImageAnalyzer
	searcher       ArticleSearcher
	messenger      Messenger
	searchMinScore float64
	logger         *slog.Logger
}

func NewAnalyzeImageService(
	fetcher PhotoFetcher,
	analyzer ImageAnalyzer,
	searcher ArticleSearcher,
	messenger Messenger,
	searchMinScore float64,
	logger *slog.Logger,
) *AnalyzeImageService {
	return &AnalyzeImageService{
		fetcher:        fetcher,
		analyzer:       analyzer,
		searcher:       searcher,
		messenger:      messenger,
		searchMinScore: searchMinScore,
		logger:         logger.With("component", "analyze_image_service"),
	}
}

func (s *AnalyzeImageService) AnalyzePhoto(ctx context.Context, msg *models.Message) {
	s.logger.InfoContext(ctx, "Получено изображение",
		"operation", "analyze_image",
		"message_id", msg.ID,
		"chat_id", msg.Chat.ID,
		"date", msg.Date,
	)

	data, mime, err := s.fetcher.FetchPhoto(ctx, msg)
	if err != nil {
		s.logger.ErrorContext(ctx, "Не удалось загрузить изображение", "message_id", msg.ID, "err", err)
		return
	}

	analysis, err := s.analyzer.AnalyzeImage(ctx, data, mime)
	if err != nil {
		s.logger.ErrorContext(ctx, "Не удалось проанализировать изображение",
			"message_id", msg.ID,
			"err", err,
			"reply_reason", "ошибка анализа LLM",
		)
		return
	}

	s.logger.InfoContext(ctx, "Анализ изображения",
		"message_id", msg.ID,
		"description", analysis.Description,
		"search_query", analysis.SearchQuery,
		"offense_detected", analysis.OffenseDetected,
		"error", analysis.Error,
	)

	if llmErr := strings.TrimSpace(analysis.Error); llmErr != "" {
		s.logger.InfoContext(ctx, "Преступление не обнаружено",
			"message_id", msg.ID,
			"reply_reason", "LLM error: "+llmErr,
		)
		return
	}
	if !analysis.OffenseDetected {
		s.logger.InfoContext(ctx, "Преступление не обнаружено",
			"message_id", msg.ID,
			"offense_detected", analysis.OffenseDetected,
			"description", analysis.Description,
		)
		return
	}

	query := buildSearchQuery(analysis)
	if query == "" {
		s.logger.InfoContext(ctx, "Пустой поисковый запрос",
			"message_id", msg.ID,
			"description", analysis.Description,
		)
		return
	}
	s.logger.InfoContext(ctx, "Поисковый запрос", "message_id", msg.ID, "query", query)
	articles, err := s.searcher.Search(ctx, query, 5)
	if err != nil {
		s.logger.ErrorContext(ctx, "Не удалось найти статью УК", "message_id", msg.ID, "err", err)
		return
	}

	best, ok := pickBestArticle(articles, s.searchMinScore)
	if !ok {
		score := 0.0
		if len(articles) > 0 {
			score = articles[0].Score
		}
		s.logger.InfoContext(ctx, "Низкая уверенность поиска",
			"message_id", msg.ID,
			"score", score,
			"min_score", s.searchMinScore,
			"reply_reason", "score ниже порога или результатов нет",
		)
		return
	}

	s.logger.InfoContext(ctx, "Статья УК найдена",
		"message_id", msg.ID,
		"article", best.Number,
		"score", best.Score,
		"min_score", s.searchMinScore,
	)
	s.sendReply(ctx, msg.Chat.ID, msg.ID, formatArticleReply(best))
}

func buildSearchQuery(analysis entities.ImageAnalysis) string {
	if q := strings.TrimSpace(analysis.SearchQuery); q != "" {
		return q
	}
	return strings.TrimSpace(analysis.Description)
}

func pickBestArticle(articles []entities.ScoredArticle, minScore float64) (entities.ScoredArticle, bool) {
	if len(articles) == 0 || articles[0].Score < minScore {
		return entities.ScoredArticle{}, false
	}
	return articles[0], true
}

func formatArticleReply(article entities.ScoredArticle) string {
	return "Статья " + article.Number + ". " + article.Title
}

func (s *AnalyzeImageService) sendReply(ctx context.Context, chatID int64, messageID int, text string) {
	if _, err := s.messenger.ReplyToChat(ctx, chatID, messageID, text); err != nil {
		s.logger.ErrorContext(ctx, "Не удалось отправить ответ", "message_id", messageID, "err", err)
	}
}
