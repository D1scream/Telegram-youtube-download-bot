package imposter

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestStoreSettingsAndTopicsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "imposter.json")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	def := DefaultSettings()
	if got := s.Settings(1, def); got != def {
		t.Fatalf("по умолчанию: %+v", got)
	}
	if added, err := s.AddTopicWords(1, "Еда", []string{"борщ", "плов", "борщ"}); err != nil || added != 2 {
		t.Fatalf("added=%d err=%v", added, err)
	}
	if added, _ := s.AddTopicWords(1, "еда", []string{"суп", "плов"}); added != 1 {
		t.Fatalf("регистр названия не должен создавать новую тему, added=%d", added)
	}
	if err := s.SetSettings(1, Settings{Rounds: 3, Tie: TieExtraRound, Master: true, Secret: true, List: TopicList("Еда")}); err != nil {
		t.Fatal(err)
	}

	again, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := again.Settings(1, def)
	if got.Rounds != 3 || got.Tie != TieExtraRound || !got.Master || got.Secret || got.List != TopicList("Еда") {
		t.Fatalf("после перезапуска: %+v", got)
	}
	words, ok := again.TopicWords(1, "ЕДА")
	slices.Sort(words)
	if !ok || !slices.Equal(words, []string{"борщ", "плов", "суп"}) {
		t.Fatalf("слова темы: %v ok=%v", words, ok)
	}
	if other := again.Settings(2, def); other != def {
		t.Fatal("настройки чатов не должны смешиваться")
	}
}

func TestStoreDeletedTopicFallsBack(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "s.json"))
	_, _ = s.AddTopicWords(1, "Еда", []string{"борщ"})
	_ = s.SetSettings(1, Settings{Rounds: 1, Tie: TieImpostorWins, List: TopicList("Еда")})
	if err := s.DeleteTopic(1, "еда"); err != nil {
		t.Fatal(err)
	}
	if got := s.Settings(1, DefaultSettings()); got.List != ListDownloaded {
		t.Fatalf("list=%q", got.List)
	}
	if err := s.DeleteTopic(1, "еда"); err != ErrNoTopic {
		t.Fatalf("got %v", err)
	}
}

func TestStoreTopicLimitsAndRemove(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "s.json"))
	if _, err := s.AddTopicWords(1, "bad:name", []string{"борщ"}); err != ErrBadTopicName {
		t.Fatalf("got %v", err)
	}
	_, _ = s.AddTopicWords(1, "Еда", []string{"борщ", "плов"})
	if n, err := s.RemoveTopicWords(1, "еда", []string{"плов", "суп"}); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	for i := 0; i < MaxTopics; i++ {
		_, _ = s.AddTopicWords(2, "т"+string(rune('а'+i)), []string{"борщ"})
	}
	if _, err := s.AddTopicWords(2, "лишняя", []string{"борщ"}); err != ErrTooManyTopics {
		t.Fatalf("got %v", err)
	}
}

func TestStoreCorruptFileIsNotOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(path, []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(path); err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if data, _ := os.ReadFile(path); string(data) != "{oops" {
		t.Fatal("файл не должен изменяться")
	}
}
