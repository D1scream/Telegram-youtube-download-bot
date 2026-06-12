package entities

type Article struct {
	Number string
	Title  string
	Text   string
}

type ScoredArticle struct {
	Article
	Score float64
}
