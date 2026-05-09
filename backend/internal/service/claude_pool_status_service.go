package service

import (
	"context"
	"errors"
	"fmt"
	htmlpkg "html"
	"io"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
)

const (
	ClaudePoolStatusFresh       = "fresh"
	ClaudePoolStatusStale       = "stale"
	ClaudePoolStatusUnavailable = "unavailable"
	ClaudePoolStatusDisabled    = "disabled"

	claudePoolFetchTimeout = 20 * time.Second
	claudePoolBodyLimit    = 5 * 1024 * 1024
)

var (
	derouterHTMLTagPattern = regexp.MustCompile(`(?s)<[^>]+>`)
	derouterSpacePattern   = regexp.MustCompile(`\s+`)
)

type ClaudePoolModelStatus struct {
	Name           string  `json:"name"`
	InputPriceUSD  float64 `json:"input_price_usd"`
	OutputPriceUSD float64 `json:"output_price_usd"`
	LoadPercent    float64 `json:"load_percent"`
	IdlePercent    float64 `json:"idle_percent"`
	Coefficient    float64 `json:"coefficient"`
}

type ClaudePoolPricingRule struct {
	IdleRange   string  `json:"idle_range"`
	Coefficient float64 `json:"coefficient"`
	Label       string  `json:"label"`
}

type ClaudePoolSnapshot struct {
	Status        string                  `json:"status"`
	SourceURL     string                  `json:"source_url"`
	UpdatedAt     time.Time               `json:"updated_at,omitempty"`
	AgeSeconds    int64                   `json:"age_seconds"`
	Stale         bool                    `json:"stale"`
	Coefficient   float64                 `json:"coefficient"`
	LoadPercent   float64                 `json:"load_percent"`
	IdlePercent   float64                 `json:"idle_percent"`
	SelectedModel string                  `json:"selected_model,omitempty"`
	StateLabel    string                  `json:"state_label"`
	Models        []ClaudePoolModelStatus `json:"models"`
	PricingRules  []ClaudePoolPricingRule `json:"pricing_rules"`
}

type ClaudePoolStatusService struct {
	cfg       config.ClaudePoolPricingConfig
	client    *http.Client
	clientErr error

	mu       sync.RWMutex
	snapshot ClaudePoolSnapshot

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
	wg        sync.WaitGroup
	now       func() time.Time
}

func NewClaudePoolStatusService(cfg *config.Config) *ClaudePoolStatusService {
	poolCfg := normalizeClaudePoolPricingConfig(cfg)
	svc := &ClaudePoolStatusService{
		cfg:    poolCfg,
		stopCh: make(chan struct{}),
		now:    time.Now,
	}
	if !poolCfg.Enabled {
		svc.snapshot = ClaudePoolSnapshot{Status: ClaudePoolStatusDisabled}
		return svc
	}

	proxyURL := ""
	allowDirectOnProxyError := false
	if cfg != nil {
		proxyURL = cfg.Update.ProxyURL
		allowDirectOnProxyError = cfg.Security.ProxyFallback.AllowDirectOnError
	}
	client, err := httpclient.GetClient(httpclient.Options{
		Timeout:  claudePoolFetchTimeout,
		ProxyURL: proxyURL,
	})
	if err != nil {
		if strings.TrimSpace(proxyURL) == "" || allowDirectOnProxyError {
			client = &http.Client{Timeout: claudePoolFetchTimeout}
		} else {
			svc.clientErr = fmt.Errorf("proxy client init failed: %w", err)
		}
	}
	svc.client = client
	svc.snapshot = ClaudePoolSnapshot{
		Status:       ClaudePoolStatusUnavailable,
		SourceURL:    poolCfg.SourceURL,
		PricingRules: DefaultClaudePoolPricingRules(),
	}
	return svc
}

func normalizeClaudePoolPricingConfig(cfg *config.Config) config.ClaudePoolPricingConfig {
	out := config.ClaudePoolPricingConfig{
		Enabled:                true,
		GroupName:              "claude满血默认",
		SourceURL:              "https://derouter.ai/pricing",
		BaseCoefficient:        0.8,
		RefreshIntervalSeconds: 300,
		StaleAfterSeconds:      1800,
	}
	if cfg != nil {
		out = cfg.Gateway.ClaudePoolPricing
	}
	out.GroupName = strings.TrimSpace(out.GroupName)
	out.SourceURL = strings.TrimSpace(out.SourceURL)
	if out.GroupName == "" {
		out.GroupName = "claude满血默认"
	}
	if out.SourceURL == "" {
		out.SourceURL = "https://derouter.ai/pricing"
	}
	if out.BaseCoefficient <= 0 {
		out.BaseCoefficient = 0.8
	}
	if out.RefreshIntervalSeconds <= 0 {
		out.RefreshIntervalSeconds = 300
	}
	if out.StaleAfterSeconds <= 0 {
		out.StaleAfterSeconds = 1800
	}
	return out
}

func ProvideClaudePoolStatusService(cfg *config.Config) *ClaudePoolStatusService {
	svc := NewClaudePoolStatusService(cfg)
	svc.Start()
	return svc
}

func (s *ClaudePoolStatusService) Start() {
	if s == nil || !s.cfg.Enabled {
		return
	}
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.refreshWithTimeout()

			interval := time.Duration(s.cfg.RefreshIntervalSeconds) * time.Second
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					s.refreshWithTimeout()
				case <-s.stopCh:
					return
				}
			}
		}()
	})
}

func (s *ClaudePoolStatusService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
		s.wg.Wait()
	})
}

func (s *ClaudePoolStatusService) refreshWithTimeout() {
	ctx, cancel := context.WithTimeout(context.Background(), claudePoolFetchTimeout)
	defer cancel()
	if err := s.RefreshOnce(ctx); err != nil {
		slog.Warn("claude pool status refresh failed", "error", err)
	}
}

func (s *ClaudePoolStatusService) RefreshOnce(ctx context.Context) error {
	if s == nil || !s.cfg.Enabled {
		return nil
	}
	if s.clientErr != nil {
		s.markRefreshFailed(s.clientErr)
		return s.clientErr
	}
	if s.client == nil {
		err := errors.New("http client is not initialized")
		s.markRefreshFailed(err)
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.SourceURL, nil)
	if err != nil {
		s.markRefreshFailed(err)
		return err
	}
	req.Header.Set("User-Agent", "Sub2API Claude Pool Status/1.0")

	resp, err := s.client.Do(req)
	if err != nil {
		s.markRefreshFailed(err)
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("derouter status HTTP %d", resp.StatusCode)
		s.markRefreshFailed(err)
		return err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, claudePoolBodyLimit))
	if err != nil {
		s.markRefreshFailed(err)
		return err
	}
	snapshot, err := ParseDerouterClaudePoolStatusHTML(string(body), s.now())
	if err != nil {
		s.markRefreshFailed(err)
		return err
	}
	snapshot.SourceURL = s.cfg.SourceURL
	snapshot.PricingRules = DefaultClaudePoolPricingRules()

	s.mu.Lock()
	s.snapshot = snapshot
	s.mu.Unlock()
	return nil
}

func (s *ClaudePoolStatusService) markRefreshFailed(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot.Status == "" {
		s.snapshot.Status = ClaudePoolStatusUnavailable
	}
	if s.snapshot.SourceURL == "" {
		s.snapshot.SourceURL = s.cfg.SourceURL
	}
	if len(s.snapshot.PricingRules) == 0 {
		s.snapshot.PricingRules = DefaultClaudePoolPricingRules()
	}
}

func (s *ClaudePoolStatusService) PublicStatus() ClaudePoolSnapshot {
	if s == nil {
		return ClaudePoolSnapshot{
			Status:       ClaudePoolStatusUnavailable,
			PricingRules: DefaultClaudePoolPricingRules(),
		}
	}

	s.mu.RLock()
	out := s.snapshot
	s.mu.RUnlock()

	if !s.cfg.Enabled {
		out.Status = ClaudePoolStatusDisabled
		out.Stale = false
		return out
	}
	if out.SourceURL == "" {
		out.SourceURL = s.cfg.SourceURL
	}
	if len(out.PricingRules) == 0 {
		out.PricingRules = DefaultClaudePoolPricingRules()
	}
	if out.UpdatedAt.IsZero() {
		out.Status = ClaudePoolStatusUnavailable
		out.Stale = true
		return out
	}

	age := s.now().Sub(out.UpdatedAt)
	if age < 0 {
		age = 0
	}
	out.AgeSeconds = int64(age.Seconds())
	out.Stale = s.isSnapshotStale(out)
	if out.Stale {
		out.Status = ClaudePoolStatusStale
	} else {
		out.Status = ClaudePoolStatusFresh
	}
	return out
}

func (s *ClaudePoolStatusService) ApplyDynamicMultiplier(baseMultiplier float64, groupName string) float64 {
	if s == nil || !s.cfg.Enabled || baseMultiplier <= 0 {
		return baseMultiplier
	}
	if strings.TrimSpace(groupName) != s.cfg.GroupName {
		return baseMultiplier
	}

	s.mu.RLock()
	snapshot := s.snapshot
	s.mu.RUnlock()
	if snapshot.Coefficient <= 0 || snapshot.UpdatedAt.IsZero() || s.isSnapshotStale(snapshot) {
		return baseMultiplier
	}
	return roundClaudePoolMultiplier(baseMultiplier * snapshot.Coefficient / s.cfg.BaseCoefficient)
}

func (s *ClaudePoolStatusService) ApplyDynamicMultiplierForModels(baseMultiplier float64, groupName string, modelNames ...string) float64 {
	if !isClaudePoolDynamicPricingModel(modelNames...) {
		return baseMultiplier
	}
	return s.ApplyDynamicMultiplier(baseMultiplier, groupName)
}

func isClaudePoolDynamicPricingModel(modelNames ...string) bool {
	for _, modelName := range modelNames {
		if strings.Contains(strings.ToLower(strings.TrimSpace(modelName)), "claude") {
			return true
		}
	}
	return false
}

func (s *ClaudePoolStatusService) isSnapshotStale(snapshot ClaudePoolSnapshot) bool {
	if snapshot.UpdatedAt.IsZero() {
		return true
	}
	staleAfter := time.Duration(s.cfg.StaleAfterSeconds) * time.Second
	if staleAfter <= 0 {
		staleAfter = 30 * time.Minute
	}
	return s.now().Sub(snapshot.UpdatedAt) > staleAfter
}

func ParseDerouterClaudePoolStatusHTML(raw string, now time.Time) (ClaudePoolSnapshot, error) {
	tokens := tokenizeDerouterHTML(raw)
	models := make([]ClaudePoolModelStatus, 0, 8)
	for i := 0; i < len(tokens); i++ {
		if !strings.EqualFold(tokens[i], "Claude") {
			continue
		}
		name, next := collectClaudeModelName(tokens, i)
		if name == "" {
			continue
		}
		inputPrice, outputPrice, afterPrices, ok := parseNextTwoMoneyTokens(tokens, next)
		if !ok {
			continue
		}
		loadPercent, coefficient, ok := parseLoadAndCoefficient(tokens, afterPrices)
		if !ok {
			continue
		}
		idlePercent := clampPercent(100 - loadPercent)
		if coefficient <= 0 {
			coefficient = coefficientForIdlePercent(idlePercent)
		}
		models = append(models, ClaudePoolModelStatus{
			Name:           name,
			InputPriceUSD:  inputPrice,
			OutputPriceUSD: outputPrice,
			LoadPercent:    loadPercent,
			IdlePercent:    idlePercent,
			Coefficient:    coefficient,
		})
	}
	if len(models) == 0 {
		return ClaudePoolSnapshot{}, errors.New("no Claude pricing rows found")
	}

	selected := models[0]
	for _, model := range models[1:] {
		if model.Coefficient > selected.Coefficient || (model.Coefficient == selected.Coefficient && model.LoadPercent > selected.LoadPercent) {
			selected = model
		}
	}

	return ClaudePoolSnapshot{
		Status:        ClaudePoolStatusFresh,
		UpdatedAt:     now,
		Stale:         false,
		Coefficient:   selected.Coefficient,
		LoadPercent:   selected.LoadPercent,
		IdlePercent:   selected.IdlePercent,
		SelectedModel: selected.Name,
		StateLabel:    claudePoolStateLabel(selected.Coefficient),
		Models:        models,
		PricingRules:  DefaultClaudePoolPricingRules(),
	}, nil
}

func tokenizeDerouterHTML(raw string) []string {
	text := strings.ReplaceAll(raw, "&nbsp;", " ")
	text = derouterHTMLTagPattern.ReplaceAllString(text, " ")
	text = htmlpkg.UnescapeString(text)
	text = strings.ReplaceAll(text, "×", " × ")
	text = strings.ReplaceAll(text, "%", " % ")
	text = derouterSpacePattern.ReplaceAllString(text, " ")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return strings.Fields(text)
}

func collectClaudeModelName(tokens []string, start int) (string, int) {
	parts := make([]string, 0, 5)
	for i := start; i < len(tokens) && len(parts) < 8; i++ {
		if isMoneyToken(tokens[i]) {
			if len(parts) < 2 {
				return "", i
			}
			return strings.Join(parts, " "), i
		}
		parts = append(parts, tokens[i])
	}
	return "", start
}

func parseNextTwoMoneyTokens(tokens []string, start int) (float64, float64, int, bool) {
	prices := make([]float64, 0, 2)
	for i := start; i < len(tokens) && i < start+16; i++ {
		value, next, ok := parseMoneyTokenAt(tokens, i)
		if !ok {
			continue
		}
		prices = append(prices, value)
		i = next - 1
		if len(prices) == 2 {
			return prices[0], prices[1], next, true
		}
	}
	return 0, 0, start, false
}

func parseLoadAndCoefficient(tokens []string, start int) (float64, float64, bool) {
	var loadPercent float64
	var coefficient float64
	hasLoad := false
	hasCoefficient := false
	limit := start + 14
	if limit > len(tokens) {
		limit = len(tokens)
	}
	for i := start; i < limit; i++ {
		if !hasLoad {
			if value, ok := parsePercentTokenAt(tokens, i); ok {
				loadPercent = clampPercent(value)
				hasLoad = true
				continue
			}
		}
		if !hasCoefficient {
			if value, ok := parseCoefficientTokenAt(tokens, i); ok {
				coefficient = value
				hasCoefficient = true
				continue
			}
		}
	}
	if hasLoad && !hasCoefficient {
		coefficient = coefficientForIdlePercent(100 - loadPercent)
		hasCoefficient = coefficient > 0
	}
	return loadPercent, coefficient, hasLoad && hasCoefficient
}

func isMoneyToken(token string) bool {
	return strings.HasPrefix(strings.TrimSpace(token), "$")
}

func parseMoneyTokenAt(tokens []string, index int) (float64, int, bool) {
	token := strings.TrimSpace(tokens[index])
	if token == "$" && index+1 < len(tokens) {
		value, ok := parsePlainFloat(tokens[index+1])
		return value, index + 2, ok
	}
	if strings.HasPrefix(token, "$") {
		value, ok := parsePlainFloat(strings.TrimPrefix(token, "$"))
		return value, index + 1, ok
	}
	return 0, index + 1, false
}

func parsePercentTokenAt(tokens []string, index int) (float64, bool) {
	token := strings.TrimSpace(tokens[index])
	if strings.HasSuffix(token, "%") {
		return parsePlainFloat(strings.TrimSuffix(token, "%"))
	}
	if index+1 < len(tokens) && strings.TrimSpace(tokens[index+1]) == "%" {
		return parsePlainFloat(token)
	}
	return 0, false
}

func parseCoefficientTokenAt(tokens []string, index int) (float64, bool) {
	token := strings.TrimSpace(tokens[index])
	lower := strings.ToLower(token)
	if strings.HasSuffix(lower, "x") || strings.HasSuffix(token, "×") {
		trimmed := strings.TrimSuffix(strings.TrimSuffix(lower, "x"), "×")
		return parsePlainFloat(trimmed)
	}
	if index+1 < len(tokens) {
		next := strings.TrimSpace(tokens[index+1])
		if next == "×" || strings.EqualFold(next, "x") {
			return parsePlainFloat(token)
		}
	}
	return 0, false
}

func parsePlainFloat(token string) (float64, bool) {
	clean := strings.TrimSpace(token)
	clean = strings.Trim(clean, "$,%×xX")
	clean = strings.ReplaceAll(clean, ",", "")
	if clean == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func coefficientForIdlePercent(idlePercent float64) float64 {
	switch {
	case idlePercent > 80:
		return 0.8
	case idlePercent >= 60:
		return 0.9
	case idlePercent >= 40:
		return 1.0
	case idlePercent >= 20:
		return 1.2
	default:
		return 1.5
	}
}

func claudePoolStateLabel(coefficient float64) string {
	switch {
	case coefficient <= 0.8:
		return "空闲充足"
	case coefficient <= 0.9:
		return "轻度繁忙"
	case coefficient <= 1.0:
		return "常规负载"
	case coefficient <= 1.2:
		return "高峰负载"
	default:
		return "峰值负载"
	}
}

func DefaultClaudePoolPricingRules() []ClaudePoolPricingRule {
	return []ClaudePoolPricingRule{
		{IdleRange: ">80%", Coefficient: 0.8, Label: "八折"},
		{IdleRange: "60-80%", Coefficient: 0.9, Label: "轻度折扣"},
		{IdleRange: "40-60%", Coefficient: 1.0, Label: "原价"},
		{IdleRange: "20-40%", Coefficient: 1.2, Label: "高峰"},
		{IdleRange: "<20%", Coefficient: 1.5, Label: "峰值"},
	}
}

func roundClaudePoolMultiplier(value float64) float64 {
	return math.Round(value*10000) / 10000
}
