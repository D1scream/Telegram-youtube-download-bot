package chess

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	nc "github.com/notnil/chess"
)

// Snapshot - то, что нужно сохранить о партии, чтобы она пережила перезапуск. Выбранная клетка не сохраняется.
type Snapshot struct {
	Host     Player `json:"host"`
	White    Player `json:"white"`
	Black    Player `json:"black"`
	Phase    Phase  `json:"phase"`
	PGN      string `json:"pgn"`
	DrawFrom int64  `json:"draw_from,omitempty"`
	Revision int    `json:"revision"`
	// MsgID и Rich - сообщение с доской и его формат, Updated - время последнего хода (для закрытия по простою).
	MsgID   int       `json:"msg_id"`
	Rich    bool      `json:"rich"`
	Updated time.Time `json:"updated"`
}

func (g *Game) Snapshot() Snapshot {
	return Snapshot{
		Host: g.host, White: g.White, Black: g.Black, Phase: g.Phase,
		PGN: g.game.String(), DrawFrom: g.drawFrom, Revision: g.Revision,
	}
}

func Restore(s Snapshot) (*Game, error) {
	g := NewGame(s.Host)
	g.White, g.Black, g.Phase, g.drawFrom, g.Revision = s.White, s.Black, s.Phase, s.DrawFrom, s.Revision
	if strings.TrimSpace(s.PGN) != "" {
		pgn, err := nc.PGN(strings.NewReader(s.PGN))
		if err != nil {
			return nil, fmt.Errorf("разобрать PGN: %w", err)
		}
		g.game = nc.NewGame(pgn)
	}
	return g, nil
}

// Store хранит активные партии по чатам в json-файле.
type Store struct {
	mu    sync.Mutex
	path  string
	games map[int64]Snapshot
}

func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, games: map[int64]Snapshot{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("прочитать %s: %w", path, err)
	}
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.games); err != nil {
		return nil, fmt.Errorf("разобрать %s: %w", path, err)
	}
	return s, nil
}

// All возвращает сохранённые партии (для восстановления при старте).
func (s *Store) All() map[int64]Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int64]Snapshot, len(s.games))
	for k, v := range s.games {
		out[k] = v
	}
	return out
}

func (s *Store) Save(chatID int64, g *Game, msgID int, rich bool, updated time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := g.Snapshot()
	snap.MsgID, snap.Rich, snap.Updated = msgID, rich, updated
	s.games[chatID] = snap
	return s.write()
}

func (s *Store) Delete(chatID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.games[chatID]; !ok {
		return nil
	}
	delete(s.games, chatID)
	return s.write()
}

func (s *Store) write() error {
	data, err := json.MarshalIndent(s.games, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
