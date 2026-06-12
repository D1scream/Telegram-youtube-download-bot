package messaging

import "errors"

var (
	ErrEmptyMessage  = errors.New("поле text обязательно")
	ErrInvalidChatID = errors.New("поле chat_id обязательно")
)
