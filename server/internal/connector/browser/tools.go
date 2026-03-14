package browser

import "github.com/cogitatorai/cogitator/server/internal/tools"

// ToolDefs returns the browser tool definitions for the Chrome connector.
func ToolDefs() []tools.ToolDef {
	return []tools.ToolDef{
		{
			Name:        "browser_list",
			Description: "List open browser tabs. Returns tab ID prefix, title, and URL for each page target.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Builtin: false,
		},
		{
			Name:        "browser_open",
			Description: "Open a new tab and navigate to a URL. Returns the target ID for subsequent commands.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "The URL to open in the new tab.",
					},
				},
				"required": []string{"url"},
			},
			Builtin: false,
		},
		{
			Name:        "browser_close",
			Description: "Close a tab. Use browser_list to find the target ID.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The target ID of the tab to close.",
					},
				},
				"required": []string{"target"},
			},
			Builtin: false,
		},
		{
			Name:        "browser_navigate",
			Description: "Navigate a tab to a new URL. Waits for page load to complete.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The target ID of the tab to navigate.",
					},
					"url": map[string]any{
						"type":        "string",
						"description": "The URL to navigate to.",
					},
				},
				"required": []string{"target", "url"},
			},
			Builtin: false,
		},
		{
			Name:        "browser_snapshot",
			Description: "Read page content as an accessibility tree. Prefer this over browser_html for understanding page structure and finding interactive elements.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The target ID of the tab to snapshot.",
					},
				},
				"required": []string{"target"},
			},
			Builtin: false,
		},
		{
			Name:        "browser_html",
			Description: "Get page HTML or a specific element's outer HTML. Truncated at 100KB. Use browser_snapshot for structured reading.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The target ID of the tab.",
					},
					"selector": map[string]any{
						"type":        "string",
						"description": "Optional CSS selector to retrieve the outer HTML of a specific element. Omit to get the full page HTML.",
					},
				},
				"required": []string{"target"},
			},
			Builtin: false,
		},
		{
			Name:        "browser_screenshot",
			Description: "Capture the viewport as a PNG image. Prints DPR for coordinate mapping. Use browser_click_xy with CSS pixels (screenshot pixels / DPR).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The target ID of the tab to screenshot.",
					},
					"file": map[string]any{
						"type":        "string",
						"description": "Optional file path to save the PNG. If omitted, the image is returned inline.",
					},
				},
				"required": []string{"target"},
			},
			Builtin: false,
		},
		{
			Name:        "browser_click",
			Description: "Click an element by CSS selector. Use browser_snapshot first to identify selectors.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The target ID of the tab.",
					},
					"selector": map[string]any{
						"type":        "string",
						"description": "CSS selector of the element to click.",
					},
				},
				"required": []string{"target", "selector"},
			},
			Builtin: false,
		},
		{
			Name:        "browser_click_xy",
			Description: "Click at specific CSS pixel coordinates. Use with DPR from browser_screenshot for precise clicking.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The target ID of the tab.",
					},
					"x": map[string]any{
						"type":        "number",
						"description": "Horizontal CSS pixel coordinate to click.",
					},
					"y": map[string]any{
						"type":        "number",
						"description": "Vertical CSS pixel coordinate to click.",
					},
				},
				"required": []string{"target", "x", "y"},
			},
			Builtin: false,
		},
		{
			Name:        "browser_type",
			Description: "Type text at the currently focused element. Use browser_click first to focus an input field.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The target ID of the tab.",
					},
					"text": map[string]any{
						"type":        "string",
						"description": "The text to type into the focused element.",
					},
				},
				"required": []string{"target", "text"},
			},
			Builtin: false,
		},
		{
			Name:        "browser_key",
			Description: "Send a keyboard event. Use after browser_type to submit forms (Enter), navigate fields (Tab), or dismiss dialogs (Escape).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The target ID of the tab.",
					},
					"key": map[string]any{
						"type":        "string",
						"description": "Key name to dispatch, e.g. Enter, Tab, Escape, ArrowDown.",
					},
				},
				"required": []string{"target", "key"},
			},
			Builtin: false,
		},
		{
			Name:        "browser_eval",
			Description: "Evaluate a JavaScript expression in the page context. Avoid index-based DOM selection across multiple calls when the DOM can change between them.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The target ID of the tab.",
					},
					"expression": map[string]any{
						"type":        "string",
						"description": "JavaScript expression to evaluate in the page context.",
					},
				},
				"required": []string{"target", "expression"},
			},
			Builtin: false,
		},
		{
			Name:        "browser_scroll",
			Description: "Scroll the page to load more content or reach elements below the fold. Use before browser_snapshot to ensure content is loaded.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The target ID of the tab.",
					},
					"direction": map[string]any{
						"type":        "string",
						"description": "Scroll direction: up or down. Defaults to down.",
						"default":     "down",
					},
					"amount": map[string]any{
						"type":        "number",
						"description": "Number of CSS pixels to scroll. Defaults to 1000.",
						"default":     1000,
					},
				},
				"required": []string{"target"},
			},
			Builtin: false,
		},
		{
			Name:        "browser_network",
			Description: "Get network resource timing entries. Shows duration, size, type, and URL for each resource loaded since page load.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The target ID of the tab.",
					},
				},
				"required": []string{"target"},
			},
			Builtin: false,
		},
		{
			Name:        "browser_load_all",
			Description: "Repeatedly click an element until it disappears from the DOM. Useful for 'Load More' buttons. Hard cap at 2 minutes.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "The target ID of the tab.",
					},
					"selector": map[string]any{
						"type":        "string",
						"description": "CSS selector for the element to click repeatedly until it disappears.",
					},
					"interval_ms": map[string]any{
						"type":        "number",
						"description": "Milliseconds to wait between clicks. Defaults to 1500.",
						"default":     1500,
					},
				},
				"required": []string{"target", "selector"},
			},
			Builtin: false,
		},
	}
}
