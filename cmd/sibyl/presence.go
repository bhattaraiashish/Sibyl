package main

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"
)

type PresenceMessages struct {
	Empty  []string
	Low    []string
	Medium []string
	High   []string
}

var (
	presenceMessages     PresenceMessages
	presenceMessagesOnce sync.Once
	messageCountReset    int64
)

func LoadPresenceMessages() {
	presenceMessagesOnce.Do(func() {
		data, err := os.ReadFile("presence.txt")
		if err != nil {
			panic(err)
		}

		var current *[]string

		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)

			if line == "" {
				continue
			}

			switch line {
			case "[empty]":
				current = &presenceMessages.Empty
			case "[low]":
				current = &presenceMessages.Low
			case "[medium]":
				current = &presenceMessages.Medium
			case "[high]":
				current = &presenceMessages.High
			default:
				if current != nil {
					*current = append(*current, line)
				}
			}
		}

		messageCountReset = time.Now().Unix()
	})
}

func UpdatePresence(ctx *BotContext) {
	LoadPresenceMessages()

	now := time.Now().Unix()
	lastMessage := ctx.LastMessageTime.Load()

	if lastMessage != 0 &&
		now-lastMessage >= int64(48*time.Hour/time.Second) {
		ctx.ResetMessageCount()
	}

	count := ctx.MessageCount.Load()

	var messages []string

	switch {
	case count == 0:
		messages = presenceMessages.Empty

	case count < 50:
		messages = presenceMessages.Low

	case count < 500:
		messages = presenceMessages.Medium

	default:
		messages = presenceMessages.High
	}

	if len(messages) == 0 {
		return
	}

	status := messages[rand.Intn(len(messages))]

	if strings.Contains(status, "%d") {
		status = fmt.Sprintf(status, count)
	}

	ctx.UpdatePresence(status)
}
