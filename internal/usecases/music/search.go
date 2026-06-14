package music

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"unicode/utf8"

	"telegram-bot/internal/entities"
)

const (
	minTrackSize = 1 << 20  // 1 MB
	maxTrackSize = 50 << 20 // 50 MB
)

type TrackSearcher interface {
	Search(ctx context.Context, query string, fileLimit int) ([]entities.Track, error)
}

type SearchMusicService struct {
	searcher         TrackSearcher
	downloader       TrackDownloader
	peerModerator    PeerModerator
	messenger        DocumentMessenger
	messageDeleter   MessageDeleter
	sessions         *chatSessionStore[entities.Track]
	pending          *pendingStore
	cacheRegistry    *userFileRegistry
	peerBanExpiry    *peerBanExpiryStore
	localSearchRoots []string
	downloadsDir     string
	fileLimit        int
	displayLimit     int
	allowedFormats   []string
	logger           *slog.Logger
}

func NewSearchMusicService(
	searcher TrackSearcher,
	downloader TrackDownloader,
	messenger DocumentMessenger,
	messageDeleter MessageDeleter,
	downloadsDir string,
	localSearchRoots []string,
	fileLimit int,
	displayLimit int,
	allowedFormats []string,
	logger *slog.Logger,
) *SearchMusicService {
	if fileLimit < 1 {
		fileLimit = 1
	}
	if displayLimit < 1 {
		displayLimit = 1
	}

	var peerModerator PeerModerator
	if moderator, ok := searcher.(PeerModerator); ok {
		peerModerator = moderator
	}

	service := &SearchMusicService{
		searcher:         searcher,
		downloader:       downloader,
		peerModerator:    peerModerator,
		messenger:        messenger,
		messageDeleter:   messageDeleter,
		sessions:         newChatSessionStore[entities.Track](),
		pending:          newPendingStore(),
		cacheRegistry:    newUserFileRegistry(downloadsDir, ".download_cache_registry.json"),
		peerBanExpiry:    newPeerBanExpiryStore(),
		localSearchRoots: localSearchRoots,
		downloadsDir:     downloadsDir,
		fileLimit:        fileLimit,
		displayLimit:     displayLimit,
		allowedFormats:   allowedFormats,
		logger:           logger.With("component", "search_music_service"),
	}
	if err := service.cacheRegistry.load(); err != nil {
		logger.Warn("Не удалось загрузить реестр кэша скачиваний", "err", err)
	}
	go service.runSessionCleanup()
	go service.runPeerBanCleanup()
	return service
}

func (s *SearchMusicService) Find(ctx context.Context, chatID int64, messageID int, query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		s.logger.InfoContext(ctx, "Пустой поисковый запрос", "operation", "find_music")
		return
	}

	s.logger.InfoContext(ctx, "Поиск музыки",
		"operation", "find_music",
		"query", query,
		"file_limit", s.fileLimit,
		"display_limit", s.displayLimit,
		"formats", s.allowedFormats,
		"local_roots", s.localSearchRoots,
	)

	localTracks, err := searchLocalTracks(query, s.localSearchRoots, s.fileLimit, s.allowedFormats)
	if err != nil {
		s.logger.WarnContext(ctx, "Не удалось выполнить локальный поиск", "query", query, "err", err)
	}

	localTracks = filterSearchTracks(localTracks, s.allowedFormats, minTrackSize, maxTrackSize)
	s.logger.InfoContext(ctx, "Локальный поиск завершён", "operation", "find_music", "query", query, "tracks", len(localTracks))

	parsed := parseQuery(query)
	remoteTracks := s.searchRemoteTracks(ctx, parsed.included)
	remoteTracks = dropExcluded(remoteTracks, parsed.excluded)

	if len(remoteTracks) == 0 {
		if bypass, ok := wildcardBypassIncluded(parsed.included); ok {
			s.logger.InfoContext(ctx, "Повтор поиска с wildcard-обходом",
				"operation", "find_music",
				"query", query,
				"bypass_query", bypass,
			)
			remoteTracks = s.searchRemoteTracks(ctx, bypass)
			remoteTracks = dropExcluded(remoteTracks, parsed.excluded)
		}
	}

	tracks := mergeWithLocalPriority(localTracks, remoteTracks, s.displayLimit)

	if len(tracks) == 0 {
		s.logger.InfoContext(ctx, "Результаты не найдены", "query", query)
		oldMessageIDs := s.sessions.Set(chatID, 0, nil)
		s.deleteSessionMessages(ctx, chatID, oldMessageIDs)
		s.trackUserMessage(chatID, messageID)
		if err := s.replyToChat(ctx, chatID, messageID, "Результаты не найдены"); err != nil {
			s.logger.ErrorContext(ctx, "Не удалось отправить ответ об отсутствии результатов", "query", query, "err", err)
		}
		return
	}

	sortRemoteTracks(tracks)

	oldMessageIDs := s.sessions.Set(chatID, 0, tracks)
	s.deleteSessionMessages(ctx, chatID, oldMessageIDs)
	s.trackUserMessage(chatID, messageID)

	if err := s.replyToChat(ctx, chatID, messageID, formatTracksReply(tracks)); err != nil {
		s.logger.ErrorContext(ctx, "Не удалось отправить результаты поиска", "query", query, "err", err)
	}
}

func filterSearchTracks(tracks []entities.Track, formats []string, minSize, maxSize int64) []entities.Track {
	tracks = filterByFormats(tracks, formats)
	tracks = filterByMinSize(tracks, minSize)
	tracks = filterByMaxSize(tracks, maxSize)
	return tracks
}

func filterByFormats(tracks []entities.Track, formats []string) []entities.Track {
	if len(formats) == 0 {
		return tracks
	}

	filtered := make([]entities.Track, 0, len(tracks))
	for _, track := range tracks {
		name := strings.ToLower(track.Filename)
		for _, format := range formats {
			if strings.HasSuffix(name, format) {
				filtered = append(filtered, track)
				break
			}
		}
	}
	return filtered
}

func sortByUploadSpeed(tracks []entities.Track) {
	slices.SortFunc(tracks, func(a, b entities.Track) int {
		return b.UploadSpeed - a.UploadSpeed
	})
}

func sortRemoteTracks(tracks []entities.Track) {
	localCount := 0
	for _, track := range tracks {
		if track.LocalPath == "" {
			break
		}
		localCount++
	}
	if localCount >= len(tracks) {
		return
	}
	remote := append([]entities.Track(nil), tracks[localCount:]...)
	sortByUploadSpeed(remote)
	copy(tracks[localCount:], remote)
}

func filterByMinSize(tracks []entities.Track, minSize int64) []entities.Track {
	filtered := make([]entities.Track, 0, len(tracks))
	for _, track := range tracks {
		if track.Size > minSize {
			filtered = append(filtered, track)
		}
	}
	return filtered
}

func filterByMaxSize(tracks []entities.Track, maxSize int64) []entities.Track {
	filtered := make([]entities.Track, 0, len(tracks))
	for _, track := range tracks {
		if track.Size <= maxSize {
			filtered = append(filtered, track)
		}
	}
	return filtered
}

func (s *SearchMusicService) searchRemoteTracks(ctx context.Context, query string) []entities.Track {
	tracks, err := s.searcher.Search(ctx, query, s.fileLimit)
	if err != nil {
		s.logger.ErrorContext(ctx, "Не удалось найти музыку в Soulseek", "query", query, "err", err)
		return nil
	}
	s.logger.InfoContext(ctx, "Поиск slskd завершён", "operation", "find_music", "query", query, "tracks", len(tracks))

	tracks = filterSearchTracks(tracks, s.allowedFormats, minTrackSize, maxTrackSize)
	if len(tracks) == 0 {
		return tracks
	}

	if banned, banErr := s.loadBlacklistedPeers(ctx); banErr != nil {
		s.logger.WarnContext(ctx, "Не удалось получить blacklist slskd", "err", banErr)
	} else {
		tracks = filterBannedPeers(tracks, banned)
	}
	return tracks
}

func (s *SearchMusicService) loadBlacklistedPeers(ctx context.Context) (map[string]struct{}, error) {
	if s.peerModerator == nil {
		return nil, nil
	}
	return s.peerModerator.GetBlacklistedPeers(ctx)
}

type parsedQuery struct {
	included string
	tokens   []string
	excluded []string
}

func parseQuery(query string) parsedQuery {
	var ins, tokens, excluded []string
	for _, word := range strings.Fields(query) {
		if len(word) > 1 && word[0] == '-' {
			excluded = append(excluded, strings.ToLower(word[1:]))
			continue
		}
		if word == "" {
			continue
		}
		ins = append(ins, word)
		token := strings.ToLower(strings.TrimPrefix(word, "*"))
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	return parsedQuery{
		included: strings.Join(ins, " "),
		tokens:   tokens,
		excluded: excluded,
	}
}

func wildcardBypassIncluded(included string) (string, bool) {
	words := strings.Fields(included)
	if len(words) == 0 {
		return included, false
	}
	out := make([]string, 0, len(words))
	changed := false
	for _, word := range words {
		if strings.HasPrefix(word, "*") {
			out = append(out, word)
			continue
		}
		for i, r := range word {
			out = append(out, "*"+word[i+utf8.RuneLen(r):])
			changed = true
			break
		}
	}
	return strings.Join(out, " "), changed
}

func dropExcluded(tracks []entities.Track, excluded []string) []entities.Track {
	if len(excluded) == 0 {
		return tracks
	}
	out := make([]entities.Track, 0, len(tracks))
	for _, track := range tracks {
		path := strings.ToLower(strings.ReplaceAll(track.Filename, "\\", "/"))
		bad := false
		for _, term := range excluded {
			if strings.Contains(path, term) {
				bad = true
				break
			}
		}
		if !bad {
			out = append(out, track)
		}
	}
	return out
}
