package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func (s *SlskdAdapter) EnsureWebhookConfigured(ctx context.Context) error {
	if s.webhookURL == "" || s.webhookSecret == "" {
		return nil
	}

	options, err := s.getRuntimeOptions(ctx)
	if err != nil {
		return err
	}
	if options.Integrations.Webhooks.TelegramBot != nil {
		return nil
	}

	return s.applyRemoteConfigYAML(ctx, func(root map[string]any) {
		patchWebhook(root, s.webhookURL, s.webhookSecret)
	})
}

func (s *SlskdAdapter) GetBlacklistedPeers(ctx context.Context) (map[string]struct{}, error) {
	members, err := s.getBlacklistedMembers(ctx)
	if err != nil {
		return nil, err
	}

	banned := make(map[string]struct{}, len(members))
	for _, username := range members {
		username = strings.ToLower(strings.TrimSpace(username))
		if username == "" {
			continue
		}
		banned[username] = struct{}{}
	}
	return banned, nil
}

func (s *SlskdAdapter) BanPeer(ctx context.Context, username string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil
	}

	return s.mutateBlacklist(ctx, func(members []string) ([]string, bool) {
		for _, member := range members {
			if strings.EqualFold(member, username) {
				return members, false
			}
		}
		return append(members, username), true
	})
}

func (s *SlskdAdapter) UnbanPeer(ctx context.Context, username string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil
	}

	return s.mutateBlacklist(ctx, func(members []string) ([]string, bool) {
		filtered := make([]string, 0, len(members))
		for _, member := range members {
			if strings.EqualFold(member, username) {
				continue
			}
			filtered = append(filtered, member)
		}
		return filtered, len(filtered) != len(members)
	})
}

func (s *SlskdAdapter) mutateBlacklist(ctx context.Context, mutate func([]string) ([]string, bool)) error {
	members, err := s.getBlacklistedMembers(ctx)
	if err != nil {
		return err
	}

	updated, changed := mutate(members)
	if !changed {
		return nil
	}

	return s.applyRemoteConfigYAML(ctx, func(root map[string]any) {
		patchBlacklist(root, updated)
	})
}

func (s *SlskdAdapter) getRuntimeOptions(ctx context.Context) (slskdOptions, error) {
	var options slskdOptions
	if err := s.doJSON(ctx, http.MethodGet, "/api/v0/options", nil, &options); err != nil {
		return slskdOptions{}, fmt.Errorf("slskd get options: %w", err)
	}
	return options, nil
}

func (s *SlskdAdapter) getBlacklistedMembers(ctx context.Context) ([]string, error) {
	options, err := s.getRuntimeOptions(ctx)
	if err != nil {
		return nil, err
	}
	if options.Transfers.Groups.Blacklisted.Members == nil {
		return []string{}, nil
	}
	return append([]string(nil), options.Transfers.Groups.Blacklisted.Members...), nil
}

func (s *SlskdAdapter) getRemoteConfigYAML(ctx context.Context) (string, error) {
	var yamlText string
	if err := s.doJSON(ctx, http.MethodGet, "/api/v0/options/yaml", nil, &yamlText); err != nil {
		return "", fmt.Errorf("slskd get remote yaml: %w", err)
	}
	return yamlText, nil
}

func (s *SlskdAdapter) applyRemoteConfigYAML(ctx context.Context, patch func(map[string]any)) error {
	current, err := s.getRemoteConfigYAML(ctx)
	if err != nil {
		return err
	}

	merged, err := mergeRemoteConfigPatches(current, patch)
	if err != nil {
		return err
	}

	body, err := json.Marshal(merged)
	if err != nil {
		return err
	}

	if err := s.doJSON(ctx, http.MethodPut, "/api/v0/options/yaml", body, nil); err != nil {
		return fmt.Errorf("slskd update remote config: %w", err)
	}
	return nil
}

func mergeRemoteConfigPatches(currentYAML string, patch func(map[string]any)) (string, error) {
	root := map[string]any{}
	if strings.TrimSpace(currentYAML) != "" {
		if err := yaml.Unmarshal([]byte(currentYAML), &root); err != nil {
			return "", fmt.Errorf("parse remote yaml: %w", err)
		}
	}

	patch(root)

	out, err := yaml.Marshal(root)
	if err != nil {
		return "", fmt.Errorf("marshal remote yaml: %w", err)
	}
	return string(out), nil
}

func patchWebhook(root map[string]any, webhookURL, webhookSecret string) {
	setPath(root, map[string]any{
		"on": []any{"DownloadFileComplete"},
		"call": map[string]any{
			"url": webhookURL,
			"headers": []any{
				map[string]any{
					"name":  "X-Webhook-Secret",
					"value": webhookSecret,
				},
			},
		},
		"timeout": 30000,
		"retry": map[string]any{
			"attempts": 3,
		},
	}, "integrations", "webhooks", "telegram_bot")
}

func patchBlacklist(root map[string]any, members []string) {
	sorted := append([]string(nil), members...)
	sort.Strings(sorted)
	setPath(root, map[string]any{"members": sorted}, "transfers", "groups", "blacklisted")
}

func setPath(root map[string]any, value any, keys ...string) {
	if len(keys) == 0 {
		return
	}

	current := root
	for _, key := range keys[:len(keys)-1] {
		next, ok := current[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[key] = next
		}
		current = next
	}
	current[keys[len(keys)-1]] = value
}
