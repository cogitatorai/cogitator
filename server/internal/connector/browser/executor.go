package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// resolveTarget finds a target ID by unique prefix match.
// prefix is case-insensitive and must match exactly one target.
func resolveTarget(prefix string, targets []TargetInfo) (string, error) {
	prefix = strings.ToLower(prefix)
	var matches []TargetInfo
	for _, t := range targets {
		if strings.HasPrefix(strings.ToLower(t.ID), prefix) {
			matches = append(matches, t)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no target matching prefix %q", prefix)
	case 1:
		return matches[0].ID, nil
	default:
		var ids []string
		for _, m := range matches {
			short := m.ID
			if len(short) > 8 {
				short = short[:8]
			}
			ids = append(ids, short)
		}
		return "", fmt.Errorf("ambiguous prefix %q matches %d targets: %s", prefix, len(matches), strings.Join(ids, ", "))
	}
}

// sessionCache maps target IDs to CDP session IDs.
type sessionCache struct {
	mu       sync.Mutex
	sessions map[string]string
}

func newSessionCache() *sessionCache {
	return &sessionCache{sessions: make(map[string]string)}
}

func (sc *sessionCache) get(targetID string) (string, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	s, ok := sc.sessions[targetID]
	return s, ok
}

func (sc *sessionCache) set(targetID, sessionID string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.sessions[targetID] = sessionID
}

func (sc *sessionCache) clear() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.sessions = make(map[string]string)
}

// getSession returns (or creates) a CDP session for the given target.
func (c *Connector) getSession(ctx context.Context, targetID string) (string, error) {
	if sid, ok := c.sessions.get(targetID); ok {
		return sid, nil
	}
	sid, err := c.client.AttachToTarget(ctx, targetID)
	if err != nil {
		return "", fmt.Errorf("attach to target: %w", err)
	}
	c.sessions.set(targetID, sid)
	return sid, nil
}

// Execute dispatches a browser_* tool call to the appropriate CDP handler.
func (c *Connector) Execute(ctx context.Context, name, args string) (string, error) {
	if !c.IsConnected() {
		return "", fmt.Errorf("browser not connected")
	}

	var params map[string]any
	if args != "" {
		if err := json.Unmarshal([]byte(args), &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
	}

	switch name {
	case "browser_list":
		return c.execList(ctx)
	case "browser_open":
		return c.execOpen(ctx, params)
	case "browser_close":
		return c.execClose(ctx, params)
	case "browser_navigate":
		return c.execNavigate(ctx, params)
	case "browser_snapshot":
		return c.execSnapshot(ctx, params)
	case "browser_html":
		return c.execHTML(ctx, params)
	case "browser_screenshot":
		return c.execScreenshot(ctx, params)
	case "browser_click":
		return c.execClick(ctx, params)
	case "browser_click_xy":
		return c.execClickXY(ctx, params)
	case "browser_type":
		return c.execType(ctx, params)
	case "browser_key":
		return c.execKey(ctx, params)
	case "browser_eval":
		return c.execEval(ctx, params)
	case "browser_scroll":
		return c.execScroll(ctx, params)
	case "browser_network":
		return c.execNetwork(ctx, params)
	case "browser_load_all":
		return c.execLoadAll(ctx, params)
	default:
		return "", fmt.Errorf("unknown browser tool: %s", name)
	}
}

// waitForLoad polls document.readyState until "complete" or timeout.
func (c *Connector) waitForLoad(ctx context.Context, sessionID string) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		result, err := c.client.Send(ctx, "Runtime.evaluate", map[string]any{
			"expression":    "document.readyState",
			"returnByValue": true,
		}, sessionID)
		if err != nil {
			return err
		}
		var resp struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		if err := json.Unmarshal(result, &resp); err == nil && resp.Result.Value == "complete" {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("page load timeout after 30s")
}

func paramString(params map[string]any, key string) string {
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func paramFloat(params map[string]any, key string, defaultVal float64) float64 {
	if v, ok := params[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		}
	}
	return defaultVal
}

// Stub implementations — filled in by Tasks 7b and 7c.

func (c *Connector) execList(ctx context.Context) (string, error) {
	targets, err := ListTargets(c.baseURL())
	if err != nil {
		return "", err
	}
	if len(targets) == 0 {
		return "No open tabs.", nil
	}
	var sb strings.Builder
	for _, t := range targets {
		prefix := t.ID
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		title := t.Title
		if len(title) > 54 {
			title = title[:54] + "..."
		}
		fmt.Fprintf(&sb, "%-10s %-57s %s\n", prefix, title, t.URL)
	}
	return sb.String(), nil
}

func (c *Connector) execOpen(ctx context.Context, params map[string]any) (string, error) {
	url := paramString(params, "url")
	if url == "" {
		return "", fmt.Errorf("url is required")
	}
	target, err := CreateTarget(c.baseURL(), url)
	if err != nil {
		return "", err
	}
	sid, err := c.getSession(ctx, target.ID)
	if err != nil {
		return "", err
	}
	c.waitForLoad(ctx, sid)
	prefix := target.ID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	return fmt.Sprintf("Opened tab %s: %s", prefix, url), nil
}

func (c *Connector) execClose(ctx context.Context, params map[string]any) (string, error) {
	targetPrefix := paramString(params, "target")
	if targetPrefix == "" {
		return "", fmt.Errorf("target is required")
	}
	targets, err := ListTargets(c.baseURL())
	if err != nil {
		return "", err
	}
	targetID, err := resolveTarget(targetPrefix, targets)
	if err != nil {
		return "", err
	}
	_, err = c.client.Send(ctx, "Target.closeTarget", map[string]any{
		"targetId": targetID,
	}, "")
	if err != nil {
		return "", err
	}
	prefix := targetID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	return fmt.Sprintf("Closed tab %s", prefix), nil
}

func (c *Connector) execNavigate(ctx context.Context, params map[string]any) (string, error) {
	targetPrefix := paramString(params, "target")
	url := paramString(params, "url")
	if targetPrefix == "" || url == "" {
		return "", fmt.Errorf("target and url are required")
	}
	targets, err := ListTargets(c.baseURL())
	if err != nil {
		return "", err
	}
	targetID, err := resolveTarget(targetPrefix, targets)
	if err != nil {
		return "", err
	}
	sid, err := c.getSession(ctx, targetID)
	if err != nil {
		return "", err
	}
	_, err = c.client.Send(ctx, "Page.navigate", map[string]any{"url": url}, sid)
	if err != nil {
		return "", err
	}
	if err := c.waitForLoad(ctx, sid); err != nil {
		return fmt.Sprintf("Navigated to %s (load timeout)", url), nil
	}
	return fmt.Sprintf("Navigated to %s", url), nil
}

func (c *Connector) execSnapshot(ctx context.Context, params map[string]any) (string, error) {
	targetPrefix := paramString(params, "target")
	if targetPrefix == "" {
		return "", fmt.Errorf("target is required")
	}
	targets, err := ListTargets(c.baseURL())
	if err != nil {
		return "", err
	}
	targetID, err := resolveTarget(targetPrefix, targets)
	if err != nil {
		return "", err
	}
	sid, err := c.getSession(ctx, targetID)
	if err != nil {
		return "", err
	}
	result, err := c.client.Send(ctx, "Accessibility.getFullAXTree", nil, sid)
	if err != nil {
		return "", fmt.Errorf("accessibility tree: %w", err)
	}
	var resp struct {
		Nodes []AXNode `json:"nodes"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("parse accessibility tree: %w", err)
	}
	tree := FormatAccessibilityTree(resp.Nodes)
	if tree == "" {
		return "Empty accessibility tree.", nil
	}
	return tree, nil
}

func (c *Connector) execHTML(ctx context.Context, params map[string]any) (string, error) {
	targetPrefix := paramString(params, "target")
	if targetPrefix == "" {
		return "", fmt.Errorf("target is required")
	}
	selector := paramString(params, "selector")
	targets, err := ListTargets(c.baseURL())
	if err != nil {
		return "", err
	}
	targetID, err := resolveTarget(targetPrefix, targets)
	if err != nil {
		return "", err
	}
	sid, err := c.getSession(ctx, targetID)
	if err != nil {
		return "", err
	}

	expr := "document.documentElement.outerHTML"
	if selector != "" {
		expr = fmt.Sprintf("(function(){ const el = document.querySelector(%q); return el ? el.outerHTML : null })()", selector)
	}
	result, err := c.client.Send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expr,
		"returnByValue": true,
	}, sid)
	if err != nil {
		return "", err
	}
	var resp struct {
		Result struct {
			Value any    `json:"value"`
			Type  string `json:"type"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", err
	}
	html, _ := resp.Result.Value.(string)
	if html == "" {
		if selector != "" {
			return fmt.Sprintf("No element found matching %q", selector), nil
		}
		return "Empty page.", nil
	}
	const maxLen = 100 * 1024
	if len(html) > maxLen {
		html = html[:maxLen] + "\n\n[truncated at 100KB]"
	}
	return html, nil
}

func (c *Connector) execScreenshot(ctx context.Context, params map[string]any) (string, error) {
	targetPrefix := paramString(params, "target")
	if targetPrefix == "" {
		return "", fmt.Errorf("target is required")
	}
	targets, err := ListTargets(c.baseURL())
	if err != nil {
		return "", err
	}
	targetID, err := resolveTarget(targetPrefix, targets)
	if err != nil {
		return "", err
	}
	sid, err := c.getSession(ctx, targetID)
	if err != nil {
		return "", err
	}

	// Get device pixel ratio
	dprResult, err := c.client.Send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    "window.devicePixelRatio",
		"returnByValue": true,
	}, sid)
	dpr := 1.0
	if err == nil {
		var dprResp struct {
			Result struct {
				Value float64 `json:"value"`
			} `json:"result"`
		}
		if json.Unmarshal(dprResult, &dprResp) == nil && dprResp.Result.Value > 0 {
			dpr = dprResp.Result.Value
		}
	}

	result, err := c.client.Send(ctx, "Page.captureScreenshot", map[string]any{
		"format": "png",
	}, sid)
	if err != nil {
		return "", fmt.Errorf("screenshot: %w", err)
	}
	var resp struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", err
	}

	imgData, err := base64.StdEncoding.DecodeString(resp.Data)
	if err != nil {
		return "", fmt.Errorf("decode screenshot: %w", err)
	}

	// Save to workspace sandbox
	fileName := paramString(params, "file")
	if fileName == "" {
		fileName = fmt.Sprintf("screenshot_%d.png", time.Now().UnixMilli())
	}
	dir := filepath.Dir(c.configPath)
	screenshotDir := filepath.Join(dir, "screenshots")
	if err := os.MkdirAll(screenshotDir, 0o755); err != nil {
		return "", fmt.Errorf("create screenshot dir: %w", err)
	}
	filePath := filepath.Join(screenshotDir, fileName)
	if err := os.WriteFile(filePath, imgData, 0o644); err != nil {
		return "", fmt.Errorf("save screenshot: %w", err)
	}

	return fmt.Sprintf("Screenshot saved: %s\nDPR: %.1f (CSS pixels = screenshot pixels / %.1f)", filePath, dpr, dpr), nil
}

func (c *Connector) execClick(ctx context.Context, params map[string]any) (string, error) {
	targetPrefix := paramString(params, "target")
	selector := paramString(params, "selector")
	if targetPrefix == "" || selector == "" {
		return "", fmt.Errorf("target and selector are required")
	}
	targets, err := ListTargets(c.baseURL())
	if err != nil {
		return "", err
	}
	targetID, err := resolveTarget(targetPrefix, targets)
	if err != nil {
		return "", err
	}
	sid, err := c.getSession(ctx, targetID)
	if err != nil {
		return "", err
	}
	js := fmt.Sprintf(`(function(){
		const el = document.querySelector(%q);
		if (!el) return JSON.stringify({ok:false});
		el.scrollIntoView({block:'center'});
		el.click();
		return JSON.stringify({ok:true, tag:el.tagName, text:el.textContent.trim().substring(0,80)});
	})()`, selector)
	result, err := c.client.Send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    js,
		"returnByValue": true,
	}, sid)
	if err != nil {
		return "", err
	}
	var resp struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", err
	}
	var clickResult struct {
		OK   bool   `json:"ok"`
		Tag  string `json:"tag"`
		Text string `json:"text"`
	}
	json.Unmarshal([]byte(resp.Result.Value), &clickResult)
	if !clickResult.OK {
		return fmt.Sprintf("No element found matching %q", selector), nil
	}
	return fmt.Sprintf("Clicked <%s> %q", clickResult.Tag, clickResult.Text), nil
}

func (c *Connector) execClickXY(ctx context.Context, params map[string]any) (string, error) {
	targetPrefix := paramString(params, "target")
	if targetPrefix == "" {
		return "", fmt.Errorf("target is required")
	}
	x := paramFloat(params, "x", 0)
	y := paramFloat(params, "y", 0)
	targets, err := ListTargets(c.baseURL())
	if err != nil {
		return "", err
	}
	targetID, err := resolveTarget(targetPrefix, targets)
	if err != nil {
		return "", err
	}
	sid, err := c.getSession(ctx, targetID)
	if err != nil {
		return "", err
	}
	// Mouse move, press, release sequence
	for _, ev := range []string{"mouseMoved", "mousePressed", "mouseReleased"} {
		p := map[string]any{
			"type": ev,
			"x":    x,
			"y":    y,
		}
		if ev == "mousePressed" || ev == "mouseReleased" {
			p["button"] = "left"
			p["clickCount"] = 1
		}
		if _, err := c.client.Send(ctx, "Input.dispatchMouseEvent", p, sid); err != nil {
			return "", fmt.Errorf("mouse %s: %w", ev, err)
		}
		if ev == "mousePressed" {
			time.Sleep(50 * time.Millisecond)
		}
	}
	return fmt.Sprintf("Clicked at (%.0f, %.0f)", x, y), nil
}

func (c *Connector) execType(ctx context.Context, params map[string]any) (string, error) {
	targetPrefix := paramString(params, "target")
	text := paramString(params, "text")
	if targetPrefix == "" || text == "" {
		return "", fmt.Errorf("target and text are required")
	}
	targets, err := ListTargets(c.baseURL())
	if err != nil {
		return "", err
	}
	targetID, err := resolveTarget(targetPrefix, targets)
	if err != nil {
		return "", err
	}
	sid, err := c.getSession(ctx, targetID)
	if err != nil {
		return "", err
	}
	_, err = c.client.Send(ctx, "Input.insertText", map[string]any{"text": text}, sid)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Typed %d characters", len(text)), nil
}

func (c *Connector) execKey(ctx context.Context, params map[string]any) (string, error) {
	targetPrefix := paramString(params, "target")
	key := paramString(params, "key")
	if targetPrefix == "" || key == "" {
		return "", fmt.Errorf("target and key are required")
	}
	targets, err := ListTargets(c.baseURL())
	if err != nil {
		return "", err
	}
	targetID, err := resolveTarget(targetPrefix, targets)
	if err != nil {
		return "", err
	}
	sid, err := c.getSession(ctx, targetID)
	if err != nil {
		return "", err
	}

	// Map key names to CDP key descriptors
	keyMap := map[string][2]string{
		"Enter":      {"\r", "Enter"},
		"Tab":        {"\t", "Tab"},
		"Escape":     {"", "Escape"},
		"Backspace":  {"", "Backspace"},
		"Delete":     {"", "Delete"},
		"ArrowUp":    {"", "ArrowUp"},
		"ArrowDown":  {"", "ArrowDown"},
		"ArrowLeft":  {"", "ArrowLeft"},
		"ArrowRight": {"", "ArrowRight"},
	}

	text, domKey := "", key
	if mapped, ok := keyMap[key]; ok {
		text = mapped[0]
		domKey = mapped[1]
	}

	// keyDown
	downParams := map[string]any{
		"type": "keyDown",
		"key":  domKey,
	}
	if text != "" {
		downParams["text"] = text
	}
	if _, err := c.client.Send(ctx, "Input.dispatchKeyEvent", downParams, sid); err != nil {
		return "", err
	}
	// keyUp
	if _, err := c.client.Send(ctx, "Input.dispatchKeyEvent", map[string]any{
		"type": "keyUp",
		"key":  domKey,
	}, sid); err != nil {
		return "", err
	}
	return fmt.Sprintf("Pressed %s", key), nil
}

func (c *Connector) execEval(ctx context.Context, params map[string]any) (string, error) {
	targetPrefix := paramString(params, "target")
	expression := paramString(params, "expression")
	if targetPrefix == "" || expression == "" {
		return "", fmt.Errorf("target and expression are required")
	}
	targets, err := ListTargets(c.baseURL())
	if err != nil {
		return "", err
	}
	targetID, err := resolveTarget(targetPrefix, targets)
	if err != nil {
		return "", err
	}
	sid, err := c.getSession(ctx, targetID)
	if err != nil {
		return "", err
	}
	result, err := c.client.Send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	}, sid)
	if err != nil {
		return "", err
	}
	var resp struct {
		Result struct {
			Type  string `json:"type"`
			Value any    `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", err
	}
	if resp.ExceptionDetails != nil {
		return fmt.Sprintf("Error: %s", resp.ExceptionDetails.Text), nil
	}
	switch v := resp.Result.Value.(type) {
	case string:
		return v, nil
	case nil:
		return "undefined", nil
	default:
		b, _ := json.Marshal(v)
		return string(b), nil
	}
}

func (c *Connector) execScroll(ctx context.Context, params map[string]any) (string, error) {
	targetPrefix := paramString(params, "target")
	if targetPrefix == "" {
		return "", fmt.Errorf("target is required")
	}
	direction := paramString(params, "direction")
	if direction == "" {
		direction = "down"
	}
	amount := paramFloat(params, "amount", 1000)
	targets, err := ListTargets(c.baseURL())
	if err != nil {
		return "", err
	}
	targetID, err := resolveTarget(targetPrefix, targets)
	if err != nil {
		return "", err
	}
	sid, err := c.getSession(ctx, targetID)
	if err != nil {
		return "", err
	}
	dy := amount
	if direction == "up" {
		dy = -amount
	}
	_, err = c.client.Send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    fmt.Sprintf("window.scrollBy(0, %f)", dy),
		"returnByValue": true,
	}, sid)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Scrolled %s by %.0f pixels", direction, amount), nil
}

func (c *Connector) execNetwork(ctx context.Context, params map[string]any) (string, error) {
	targetPrefix := paramString(params, "target")
	if targetPrefix == "" {
		return "", fmt.Errorf("target is required")
	}
	targets, err := ListTargets(c.baseURL())
	if err != nil {
		return "", err
	}
	targetID, err := resolveTarget(targetPrefix, targets)
	if err != nil {
		return "", err
	}
	sid, err := c.getSession(ctx, targetID)
	if err != nil {
		return "", err
	}
	result, err := c.client.Send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    "JSON.stringify(performance.getEntriesByType('resource').map(e=>({name:e.name,duration:Math.round(e.duration),size:e.transferSize,type:e.initiatorType})))",
		"returnByValue": true,
	}, sid)
	if err != nil {
		return "", err
	}
	var resp struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", err
	}
	var entries []struct {
		Name     string `json:"name"`
		Duration int    `json:"duration"`
		Size     int    `json:"size"`
		Type     string `json:"type"`
	}
	json.Unmarshal([]byte(resp.Result.Value), &entries)
	if len(entries) == 0 {
		return "No network entries.", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-8s %-8s %-12s %s\n", "ms", "bytes", "type", "url")
	for _, e := range entries {
		url := e.Name
		if len(url) > 80 {
			url = url[:80] + "..."
		}
		fmt.Fprintf(&sb, "%-8d %-8d %-12s %s\n", e.Duration, e.Size, e.Type, url)
	}
	return sb.String(), nil
}

func (c *Connector) execLoadAll(ctx context.Context, params map[string]any) (string, error) {
	targetPrefix := paramString(params, "target")
	selector := paramString(params, "selector")
	if targetPrefix == "" || selector == "" {
		return "", fmt.Errorf("target and selector are required")
	}
	intervalMs := paramFloat(params, "interval_ms", 1500)
	targets, err := ListTargets(c.baseURL())
	if err != nil {
		return "", err
	}
	targetID, err := resolveTarget(targetPrefix, targets)
	if err != nil {
		return "", err
	}
	sid, err := c.getSession(ctx, targetID)
	if err != nil {
		return "", err
	}

	deadline := time.Now().Add(2 * time.Minute)
	clicks := 0
	for time.Now().Before(deadline) && ctx.Err() == nil {
		js := fmt.Sprintf(`(function(){
			const el = document.querySelector(%q);
			if (!el) return "gone";
			el.scrollIntoView({block:'center'});
			el.click();
			return "clicked";
		})()`, selector)
		result, err := c.client.Send(ctx, "Runtime.evaluate", map[string]any{
			"expression":    js,
			"returnByValue": true,
		}, sid)
		if err != nil {
			return "", err
		}
		var resp struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		json.Unmarshal(result, &resp)
		if resp.Result.Value != "clicked" {
			break
		}
		clicks++
		time.Sleep(time.Duration(intervalMs) * time.Millisecond)
	}
	return fmt.Sprintf("Clicked %q %d times until element was no longer found.", selector, clicks), nil
}
