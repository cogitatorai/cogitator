package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/provider"
)

// jsonIDs serialises a slice of IDs into a JSON array string for use as a
// mock provider response.
func jsonIDs(ids ...string) string {
	b, _ := json.Marshal(ids)
	return string(b)
}

// TestRetrieverEmptyGraph verifies that Retrieve returns an empty context
// without invoking the LLM when the store contains no nodes.
func TestRetrieverEmptyGraph(t *testing.T) {
	store := NewStore(testDB(t))
	mock := provider.NewMock()

	r := NewRetriever(RetrieverConfig{
		Store:    store,
		Provider: mock,
		Model:    "test-model",
	})

	ctx := context.Background()
	got, err := r.Retrieve(ctx, "", "anything", nil)
	if err != nil {
		t.Fatalf("Retrieve() error: %v", err)
	}
	if len(got.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(got.Nodes))
	}
	if len(got.Connected) != 0 {
		t.Errorf("expected 0 connected, got %d", len(got.Connected))
	}
	// The LLM must not have been called.
	if n := mock.CallCount(); n != 0 {
		t.Errorf("expected 0 provider calls, got %d", n)
	}
}

// TestRetrieverClassifiesAndLoads creates three nodes with content files,
// mocks the provider to return two of the three IDs, and verifies the correct
// nodes are loaded with their content present.
func TestRetrieverClassifiesAndLoads(t *testing.T) {
	dir := t.TempDir()
	db := testDB(t)
	store := NewStore(db)
	cm := NewContentManager(dir)

	// Create nodes without content paths first to obtain their IDs.
	idFact, err := store.CreateNode(&Node{
		Type:       NodeFact,
		Title:      "Go is compiled",
		Summary:    "Go compiles to native binaries",
		Confidence: 0.9,
	})
	if err != nil {
		t.Fatalf("CreateNode fact: %v", err)
	}

	idPref, err := store.CreateNode(&Node{
		Type:       NodePreference,
		Title:      "Prefers short functions",
		Summary:    "User likes functions under 30 lines",
		Confidence: 0.8,
	})
	if err != nil {
		t.Fatalf("CreateNode preference: %v", err)
	}

	idPat, err := store.CreateNode(&Node{
		Type:       NodePattern,
		Title:      "Uses table-driven tests",
		Summary:    "User consistently writes table-driven tests",
		Confidence: 0.7,
	})
	if err != nil {
		t.Fatalf("CreateNode pattern: %v", err)
	}

	// Write content files and update ContentPath on each node.
	for _, pair := range []struct {
		id      string
		content string
	}{
		{idFact, "Go compiles to a single static binary with no runtime dependencies."},
		{idPref, "Keep functions focused and under 30 lines where possible."},
		{idPat, "Use subtests and a slice of test cases to keep tests DRY."},
	} {
		relPath, wErr := cm.Write(pair.id, pair.content)
		if wErr != nil {
			t.Fatalf("cm.Write(%s): %v", pair.id, wErr)
		}
		node, gErr := store.GetNode(pair.id)
		if gErr != nil {
			t.Fatalf("GetNode(%s): %v", pair.id, gErr)
		}
		node.ContentPath = relPath
		if uErr := store.UpdateNode(node); uErr != nil {
			t.Fatalf("UpdateNode(%s): %v", pair.id, uErr)
		}
	}

	// Mock returns idFact and idPref; idPat is not relevant to the message.
	mock := provider.NewMock(provider.Response{Content: jsonIDs(idFact, idPref)})

	r := NewRetriever(RetrieverConfig{
		Store:    store,
		Content:  cm,
		Provider: mock,
		Model:    "test-model",
		TopK:     5,
	})

	got, err := r.Retrieve(context.Background(), "", "Tell me about Go compilation", nil)
	if err != nil {
		t.Fatalf("Retrieve() error: %v", err)
	}

	if len(got.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(got.Nodes))
	}

	// Verify the correct nodes were loaded.
	ids := make(map[string]bool)
	for _, n := range got.Nodes {
		ids[n.Node.ID] = true
		if n.Content == "" {
			t.Errorf("expected non-empty content for node %s", n.Node.ID)
		}
	}
	if !ids[idFact] {
		t.Errorf("expected fact node %s in results", idFact)
	}
	if !ids[idPref] {
		t.Errorf("expected preference node %s in results", idPref)
	}
	if ids[idPat] {
		t.Errorf("pattern node %s should not be in results", idPat)
	}

	// Verify the provider was called exactly once with a non-empty prompt.
	if n := mock.CallCount(); n != 1 {
		t.Errorf("expected 1 provider call, got %d", n)
	}
}

// TestRetrieverFollowsEdges creates two nodes connected by a high-weight edge.
// The mock returns only the first node's ID. The second node must appear in
// Connected summaries because the edge weight meets the threshold.
func TestRetrieverFollowsEdges(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)

	idA, err := store.CreateNode(&Node{
		Type:       NodeFact,
		Title:      "Node A",
		Summary:    "Summary of A",
		Confidence: 0.8,
	})
	if err != nil {
		t.Fatalf("CreateNode A: %v", err)
	}

	idB, err := store.CreateNode(&Node{
		Type:       NodeFact,
		Title:      "Node B",
		Summary:    "Summary of B",
		Confidence: 0.7,
	})
	if err != nil {
		t.Fatalf("CreateNode B: %v", err)
	}

	if err := store.CreateEdge(&Edge{
		SourceID: idA,
		TargetID: idB,
		Relation: RelSupports,
		Weight:   0.9, // above default threshold of 0.5
	}); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}

	// Classifier returns only A.
	mock := provider.NewMock(provider.Response{Content: jsonIDs(idA)})

	r := NewRetriever(RetrieverConfig{
		Store:    store,
		Provider: mock,
		Model:    "test-model",
	})

	got, err := r.Retrieve(context.Background(), "", "tell me about A", nil)
	if err != nil {
		t.Fatalf("Retrieve() error: %v", err)
	}

	if len(got.Nodes) != 1 {
		t.Fatalf("expected 1 primary node, got %d", len(got.Nodes))
	}
	if got.Nodes[0].Node.ID != idA {
		t.Errorf("expected node A, got %s", got.Nodes[0].Node.ID)
	}

	if len(got.Connected) != 1 {
		t.Fatalf("expected 1 connected node, got %d", len(got.Connected))
	}
	if got.Connected[0].ID != idB {
		t.Errorf("expected connected node B (%s), got %s", idB, got.Connected[0].ID)
	}
	if got.Connected[0].Title != "Node B" {
		t.Errorf("expected title 'Node B', got %q", got.Connected[0].Title)
	}
}

// TestRetrieverFollowsEdgesAboveThreshold verifies that edges below the
// minimum weight threshold are not followed.
func TestRetrieverFollowsEdgesAboveThreshold(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)

	idA, _ := store.CreateNode(&Node{Type: NodeFact, Title: "A", Confidence: 0.8})
	idB, _ := store.CreateNode(&Node{Type: NodeFact, Title: "B", Confidence: 0.7})

	// Edge weight below default threshold.
	store.CreateEdge(&Edge{
		SourceID: idA,
		TargetID: idB,
		Relation: RelRelatedTo,
		Weight:   0.3,
	})

	mock := provider.NewMock(provider.Response{Content: jsonIDs(idA)})

	r := NewRetriever(RetrieverConfig{
		Store:         store,
		Provider:      mock,
		Model:         "test-model",
		EdgeMinWeight: 0.5,
	})

	got, err := r.Retrieve(context.Background(), "", "query", nil)
	if err != nil {
		t.Fatalf("Retrieve() error: %v", err)
	}
	if len(got.Connected) != 0 {
		t.Errorf("expected 0 connected nodes for low-weight edge, got %d", len(got.Connected))
	}
}

// TestRetrieverTopKLimit creates more nodes than topK. The mock returns all
// IDs. Only topK nodes must appear in the result.
func TestRetrieverTopKLimit(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)

	var allIDs []string
	for i := 0; i < 8; i++ {
		id, err := store.CreateNode(&Node{
			Type:       NodeFact,
			Title:      "Node",
			Confidence: 0.5,
		})
		if err != nil {
			t.Fatalf("CreateNode %d: %v", i, err)
		}
		allIDs = append(allIDs, id)
	}

	// Mock returns all 8 IDs.
	mock := provider.NewMock(provider.Response{Content: jsonIDs(allIDs...)})

	const topK = 3
	r := NewRetriever(RetrieverConfig{
		Store:    store,
		Provider: mock,
		Model:    "test-model",
		TopK:     topK,
	})

	got, err := r.Retrieve(context.Background(), "", "general query", nil)
	if err != nil {
		t.Fatalf("Retrieve() error: %v", err)
	}
	if len(got.Nodes) != topK {
		t.Errorf("expected %d nodes (topK), got %d", topK, len(got.Nodes))
	}
	// The loaded nodes must be the first topK from the ranked list.
	for i := 0; i < topK; i++ {
		if got.Nodes[i].Node.ID != allIDs[i] {
			t.Errorf("node[%d]: expected %s, got %s", i, allIDs[i], got.Nodes[i].Node.ID)
		}
	}
}

// TestRetrieverContextString verifies the String() output format of a
// manually constructed RetrievedContext.
func TestRetrieverContextString(t *testing.T) {
	rc := RetrievedContext{
		Nodes: []RetrievedNode{
			{
				Node:    Node{Title: "Go Compilation", Type: NodeFact, Summary: "unused when content present"},
				Content: "Go compiles to native code.",
			},
			{
				Node:    Node{Title: "Prefers Tabs", Type: NodePreference, Summary: "User prefers tabs over spaces"},
				Content: "", // falls back to Summary
			},
		},
		Connected: []NodeSummary{
			{Title: "Toolchain Details", Summary: "The Go toolchain includes go build and go test"},
		},
	}

	s := rc.String()

	// Node titles must be present as subsection headings (#### under ### Retrieved Memories).
	if !strings.Contains(s, "#### Go Compilation") {
		t.Error("missing node title heading")
	}

	// Section header for retrieved memories.
	if !strings.Contains(s, "### Retrieved Memories") {
		t.Error("missing '### Retrieved Memories' section header")
	}

	// Node with content: content must be used, not summary.
	if !strings.Contains(s, "Go compiles to native code.") {
		t.Error("expected node content in output")
	}
	if strings.Contains(s, "unused when content present") {
		t.Error("summary must not appear when content is present")
	}

	// Node without content: summary must be used.
	if !strings.Contains(s, "User prefers tabs over spaces") {
		t.Error("expected node summary as fallback when content is empty")
	}

	// Node type must appear in the heading.
	if !strings.Contains(s, "(fact)") {
		t.Error("expected node type 'fact' in heading")
	}
	if !strings.Contains(s, "(preference)") {
		t.Error("expected node type 'preference' in heading")
	}

	// Connected section must be present.
	if !strings.Contains(s, "### Related Knowledge") {
		t.Error("missing '### Related Knowledge' section")
	}
	if !strings.Contains(s, "Toolchain Details") {
		t.Error("expected connected node title in output")
	}
	if !strings.Contains(s, "The Go toolchain includes go build and go test") {
		t.Error("expected connected node summary in output")
	}
}

// TestRetrieverContextStringEmpty verifies that String() returns an empty
// string when there are no nodes.
func TestRetrieverContextStringEmpty(t *testing.T) {
	rc := RetrievedContext{}
	if s := rc.String(); s != "" {
		t.Errorf("expected empty string, got %q", s)
	}
}

// TestRetrieverContextStringConnectedNoSummary verifies that connected nodes
// without a summary are rendered without a trailing colon.
func TestRetrieverContextStringConnectedNoSummary(t *testing.T) {
	rc := RetrievedContext{
		Nodes: []RetrievedNode{
			{Node: Node{Title: "A", Type: NodeFact}, Content: "content"},
		},
		Connected: []NodeSummary{
			{Title: "B", Summary: ""},
		},
	}
	s := rc.String()
	if strings.Contains(s, "B:") {
		t.Errorf("connected node without summary must not have a colon, got: %s", s)
	}
	if !strings.Contains(s, "- B\n") {
		t.Errorf("expected '- B' line, got: %s", s)
	}
}

// TestRetrieverContextStringPinned verifies that pinned nodes render in the
// Pinned Memories section and non-pinned in the Retrieved Memories section.
func TestRetrieverContextStringPinned(t *testing.T) {
	rc := RetrievedContext{
		Pinned: []RetrievedNode{
			{Node: Node{Title: "Always Present", Type: NodeFact}, Content: "pinned content"},
		},
		Nodes: []RetrievedNode{
			{Node: Node{Title: "Retrieved Node", Type: NodePreference}, Content: "retrieved content"},
		},
	}
	s := rc.String()

	if !strings.Contains(s, "### Pinned Memories") {
		t.Error("missing '### Pinned Memories' section")
	}
	if !strings.Contains(s, "#### Always Present") {
		t.Error("missing pinned node title")
	}
	if !strings.Contains(s, "pinned content") {
		t.Error("missing pinned node content")
	}
	if !strings.Contains(s, "### Retrieved Memories") {
		t.Error("missing '### Retrieved Memories' section")
	}
	if !strings.Contains(s, "#### Retrieved Node") {
		t.Error("missing retrieved node title")
	}
	if !strings.Contains(s, "retrieved content") {
		t.Error("missing retrieved node content")
	}
}

// TestRetrieverTwoStageAssociation verifies that when a standard provider is
// configured, the retriever runs two-stage retrieval: stage 1 expands
// associations, stage 2 uses them to select nodes.
func TestRetrieverTwoStageAssociation(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)

	idPref, err := store.CreateNode(&Node{
		Type:              NodePreference,
		Title:             "Likes Lord of the Rings",
		Summary:           "User enjoys Tolkien's work",
		Confidence:        0.9,
		RetrievalTriggers: []string{"tolkien", "fantasy", "lord of the rings"},
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	idFact, err := store.CreateNode(&Node{
		Type:       NodeFact,
		Title:      "User lives in London",
		Summary:    "Primary residence is London, UK",
		Confidence: 0.8,
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	// Stage 1 mock (standard provider): returns associations that bridge
	// "New Zealand trip" to "Lord of the Rings".
	stage1Mock := provider.NewMock(provider.Response{
		Content: `["new zealand", "travel", "lord of the rings filming locations", "hiking"]`,
	})

	// Stage 2 mock (cheap provider): given the associations, selects the LOTR preference.
	stage2Mock := provider.NewMock(provider.Response{
		Content: jsonIDs(idPref),
	})

	r := NewRetriever(RetrieverConfig{
		Store:            store,
		Provider:         stage2Mock,
		Model:            "cheap-model",
		StandardProvider: stage1Mock,
		StandardModel:    "standard-model",
	})

	got, err := r.Retrieve(context.Background(), "", "Recommend things to do on a trip to New Zealand", nil)
	if err != nil {
		t.Fatalf("Retrieve() error: %v", err)
	}

	// Stage 1 (standard) must have been called once.
	if n := stage1Mock.CallCount(); n != 1 {
		t.Errorf("expected 1 stage-1 call, got %d", n)
	}

	// Stage 2 (cheap) must have been called once.
	if n := stage2Mock.CallCount(); n != 1 {
		t.Errorf("expected 1 stage-2 call, got %d", n)
	}

	// The LOTR preference node must be retrieved.
	if len(got.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(got.Nodes))
	}
	if got.Nodes[0].Node.ID != idPref {
		t.Errorf("expected LOTR preference node %s, got %s", idPref, got.Nodes[0].Node.ID)
	}

	// The fact node about London must NOT be retrieved.
	for _, n := range got.Nodes {
		if n.Node.ID == idFact {
			t.Errorf("London fact node %s should not be in results", idFact)
		}
	}

	// Verify stage 2 prompt includes associations.
	stage2Calls := stage2Mock.Calls
	if len(stage2Calls) > 0 {
		prompt := stage2Calls[0][len(stage2Calls[0])-1].ContentText()
		if !strings.Contains(prompt, "lord of the rings filming locations") {
			t.Error("stage 2 prompt should contain expanded associations")
		}
	}
}

// TestRetrieverFallbackSingleStage verifies that when no standard provider
// is configured, the retriever falls back to single-stage classification
// without calling expandAssociations.
func TestRetrieverFallbackSingleStage(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)

	id, err := store.CreateNode(&Node{
		Type:       NodeFact,
		Title:      "Test node",
		Confidence: 0.8,
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	// Only cheap provider set, no standard provider.
	cheapMock := provider.NewMock(provider.Response{Content: jsonIDs(id)})

	r := NewRetriever(RetrieverConfig{
		Store:    store,
		Provider: cheapMock,
		Model:    "cheap-model",
		// StandardProvider intentionally nil
	})

	got, err := r.Retrieve(context.Background(), "", "anything", nil)
	if err != nil {
		t.Fatalf("Retrieve() error: %v", err)
	}

	// Only one LLM call (the cheap classifier), no stage 1.
	if n := cheapMock.CallCount(); n != 1 {
		t.Errorf("expected 1 provider call (single-stage fallback), got %d", n)
	}

	if len(got.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(got.Nodes))
	}
}

// TestRetrieveWithEmbedder verifies that when an embedder is configured the
// retriever uses vector similarity to select the most relevant nodes rather
// than calling the LLM classifier.
func TestRetrieveWithEmbedder(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)

	// Create 3 nodes.
	idA, err := store.CreateNode(&Node{Type: NodeFact, Title: "Go language", Summary: "Compiled systems language", Confidence: 0.9})
	if err != nil {
		t.Fatalf("CreateNode A: %v", err)
	}
	idB, err := store.CreateNode(&Node{Type: NodeFact, Title: "Python language", Summary: "Interpreted scripting language", Confidence: 0.8})
	if err != nil {
		t.Fatalf("CreateNode B: %v", err)
	}
	idC, err := store.CreateNode(&Node{Type: NodeFact, Title: "Cooking recipes", Summary: "User likes Italian cuisine", Confidence: 0.7})
	if err != nil {
		t.Fatalf("CreateNode C: %v", err)
	}

	// The mock embedder returns [0.1, 0.2, 0.3] for any text.
	// Design stored embeddings so A and B score high (near-parallel to query)
	// and C scores low (near-opposite direction).
	// Query [0.1, 0.2, 0.3]: normQ = sqrt(0.14)
	// A [0.1, 0.2, 0.3]: sim = 1.0
	// B [0.2, 0.3, 0.1]: sim ~= 0.786
	// C [-1.0, 0.0, 0.0]: sim ~= -0.267 (negative, clearly worst)
	store.SaveEmbedding(idA, []float32{0.1, 0.2, 0.3}, "test-model") // sim = 1.0
	store.SaveEmbedding(idB, []float32{0.2, 0.3, 0.1}, "test-model") // sim ~= 0.786
	store.SaveEmbedding(idC, []float32{-1.0, 0.0, 0.0}, "test-model") // sim ~= -0.267

	// Mock embedder always returns [0.1, 0.2, 0.3] for any text.
	mockEmb := provider.NewMock()

	// No LLM provider: vector path must not call LLM.
	llmMock := provider.NewMock()

	r := NewRetriever(RetrieverConfig{
		Store:          store,
		Provider:       llmMock,
		Model:          "test-model",
		Embedder:       mockEmb,
		EmbeddingModel: "test-model",
		TopK:           2, // only top 2
	})

	got, err := r.Retrieve(context.Background(), "", "Tell me about Go", nil)
	if err != nil {
		t.Fatalf("Retrieve() error: %v", err)
	}

	// LLM must not have been called.
	if n := llmMock.CallCount(); n != 0 {
		t.Errorf("LLM must not be called when embedder is configured, got %d calls", n)
	}

	// Exactly 2 nodes (topK).
	if len(got.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(got.Nodes))
	}

	// idC (orthogonal to query) must not be in results.
	for _, n := range got.Nodes {
		if n.Node.ID == idC {
			t.Errorf("cooking node %s should not be in top-2 results", idC)
		}
	}

	// idA and idB must be present.
	ids := make(map[string]bool)
	for _, n := range got.Nodes {
		ids[n.Node.ID] = true
	}
	if !ids[idA] {
		t.Errorf("expected Go node %s in results", idA)
	}
	if !ids[idB] {
		t.Errorf("expected Python node %s in results", idB)
	}
}

// TestRetrieveWithEmbedderAndHistory verifies that history messages are
// incorporated into the retrieval query text.
func TestRetrieveWithEmbedderAndHistory(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)

	id, err := store.CreateNode(&Node{Type: NodeFact, Title: "Node", Confidence: 0.9})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	store.SaveEmbedding(id, []float32{0.1, 0.2, 0.3}, "m")

	mockEmb := provider.NewMock()
	r := NewRetriever(RetrieverConfig{
		Store:          store,
		Embedder:       mockEmb,
		EmbeddingModel: "m",
		TopK:           5,
		ContextWindow:  3,
	})

	history := []provider.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}

	got, err := r.Retrieve(context.Background(), "", "Follow-up question", history)
	if err != nil {
		t.Fatalf("Retrieve() error: %v", err)
	}

	// Should complete without error and return nodes.
	if len(got.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(got.Nodes))
	}
}

// TestRetrievedContextFormat verifies that Format annotates memories owned by
// a specific user with "shared by {name}" while leaving truly shared memories
// (nil UserID) without attribution.
func TestRetrievedContextFormat(t *testing.T) {
	bobID := "user_bob"
	rc := RetrievedContext{
		Nodes: []RetrievedNode{
			{
				Node:    Node{Title: "Birthday", Type: "fact", UserID: &bobID},
				Content: "December 12th",
			},
			{
				Node:    Node{Title: "App timezone", Type: "fact"},
				Content: "Europe/Paris",
			},
		},
	}

	resolve := func(uid string) string {
		switch uid {
		case "user_bob":
			return "Bob"
		case "user_alice":
			return "Alice"
		}
		return ""
	}

	result := rc.Format(resolve, "user_alice")

	if !strings.Contains(result, "Memory Instructions") {
		t.Error("expected memory instructions preamble")
	}
	if !strings.Contains(result, "shared by Bob") {
		t.Errorf("expected 'shared by Bob' in output, got:\n%s", result)
	}
	// Shared memory (nil UserID) should not have "shared by" attribution.
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		if strings.Contains(line, "App timezone") && strings.Contains(line, "shared by") {
			t.Error("shared memory should not have 'shared by' attribution")
		}
	}
	// Unattributed memory (no SubjectID) should NOT be assumed to be about
	// any user. Only memories with an explicit SubjectID get "about X".
	for _, line := range lines {
		if strings.Contains(line, "App timezone") && strings.Contains(line, "about") {
			t.Errorf("unattributed memory should not have 'about' annotation, got:\n%s", line)
		}
	}
}

// TestRetrievedContextFormatNilResolver verifies that Format with a nil
// resolver produces no attribution, matching the String() behavior.
func TestRetrievedContextFormatNilResolver(t *testing.T) {
	bobID := "user_bob"
	rc := RetrievedContext{
		Nodes: []RetrievedNode{
			{Node: Node{Title: "Test", Type: "fact", UserID: &bobID}, Content: "data"},
		},
	}

	result := rc.Format(nil, "")

	if strings.Contains(result, "shared by") {
		t.Error("nil resolver should not produce attribution")
	}
}

// TestBuildRetrievalText verifies that history is trimmed to contextWindow and
// the message is appended with the User: prefix.
func TestBuildRetrievalText(t *testing.T) {
	history := []provider.Message{
		{Role: "user", Content: "msg1"},
		{Role: "assistant", Content: "msg2"},
		{Role: "user", Content: "msg3"},
		{Role: "assistant", Content: "msg4"},
	}

	// contextWindow=2: only last 2 history messages + current message.
	text := buildRetrievalText("current", history, 2)

	if strings.Contains(text, "msg1") {
		t.Error("msg1 should be trimmed by contextWindow=2")
	}
	if strings.Contains(text, "msg2") {
		t.Error("msg2 should be trimmed by contextWindow=2")
	}
	if !strings.Contains(text, "msg3") {
		t.Error("msg3 should be included")
	}
	if !strings.Contains(text, "msg4") {
		t.Error("msg4 should be included")
	}
	if !strings.Contains(text, "User: current") {
		t.Errorf("current message missing, got: %s", text)
	}
}

func TestRetrieverTypeFiltering(t *testing.T) {
	dir := t.TempDir()
	db := testDB(t)
	store := NewStore(db)
	cm := NewContentManager(dir)
	mock := provider.NewMock()

	idPref, _ := store.CreateNode(&Node{Type: NodePreference, Title: "likes coffee"})
	idEpisode, _ := store.CreateNode(&Node{Type: NodeEpisode, Title: "went to store"})

	for _, id := range []string{idPref, idEpisode} {
		node, _ := store.GetNode(id)
		cm.Write(id, "content for "+node.Title)
		store.SaveEmbedding(id, []float32{0.5, 0.5, 0.5}, "test-model")
		store.UpdateContentLength(id, 100)
	}

	r := NewRetriever(RetrieverConfig{
		Store:   store,
		Content: cm,
		Logger:  slog.Default(),
		Types:   []NodeType{NodePreference, NodeFact, NodePattern, NodeSkill},
	})
	r.SetEmbedder(mock, "test-model")

	got, err := r.Retrieve(context.Background(), "", "coffee order", nil)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	for _, n := range got.Nodes {
		if n.Node.Type == NodeEpisode {
			t.Errorf("episode node should not appear in results")
		}
	}
	if len(got.Nodes) == 0 {
		t.Error("expected at least one result")
	}
}

func TestRetrieverTokenBudget(t *testing.T) {
	dir := t.TempDir()
	db := testDB(t)
	store := NewStore(db)
	cm := NewContentManager(dir)
	mock := provider.NewMock()

	content := strings.Repeat("x", 500)
	for i := 0; i < 5; i++ {
		id, _ := store.CreateNode(&Node{Type: NodeFact, Title: fmt.Sprintf("fact %d", i)})
		path, _ := cm.Write(id, content)
		node, _ := store.GetNode(id)
		node.ContentPath = path
		store.UpdateNode(node)
		store.UpdateContentLength(id, len(content))
		store.SaveEmbedding(id, []float32{0.5, 0.5, 0.5}, "test-model")
	}

	r := NewRetriever(RetrieverConfig{
		Store:       store,
		Content:     cm,
		Logger:      slog.Default(),
		TokenBudget: 400,
		TopK:        20,
	})
	r.SetEmbedder(mock, "test-model")

	got, err := r.Retrieve(context.Background(), "", "anything", nil)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(got.Nodes) > 3 {
		t.Errorf("expected at most 3 nodes within budget, got %d", len(got.Nodes))
	}
	if len(got.Nodes) == 0 {
		t.Error("expected at least one result")
	}
}

func TestRetrieverSimilarityThreshold(t *testing.T) {
	dir := t.TempDir()
	db := testDB(t)
	store := NewStore(db)
	cm := NewContentManager(dir)
	mock := provider.NewMock()

	idHigh, _ := store.CreateNode(&Node{Type: NodeFact, Title: "relevant"})
	store.SaveEmbedding(idHigh, []float32{0.9, 0.1, 0.0}, "test-model")
	store.UpdateContentLength(idHigh, 100)

	idLow, _ := store.CreateNode(&Node{Type: NodeFact, Title: "irrelevant"})
	store.SaveEmbedding(idLow, []float32{0.0, 0.0, 1.0}, "test-model")
	store.UpdateContentLength(idLow, 100)

	r := NewRetriever(RetrieverConfig{
		Store:         store,
		Content:       cm,
		Logger:        slog.Default(),
		MinSimilarity: 0.3,
	})
	mock.EmbedResponse = [][]float32{{0.9, 0.1, 0.0}}
	r.SetEmbedder(mock, "test-model")

	got, err := r.Retrieve(context.Background(), "", "relevant query", nil)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	for _, n := range got.Nodes {
		if n.Node.ID == idLow {
			t.Error("low-similarity node should have been filtered by threshold")
		}
	}
}

func TestRetrieverTypeBoost(t *testing.T) {
	dir := t.TempDir()
	db := testDB(t)
	store := NewStore(db)
	cm := NewContentManager(dir)
	mock := provider.NewMock()

	idPattern, _ := store.CreateNode(&Node{Type: NodePattern, Title: "pattern"})
	store.SaveEmbedding(idPattern, []float32{0.70, 0.30, 0.0}, "test-model")
	store.UpdateContentLength(idPattern, 100)

	idPref, _ := store.CreateNode(&Node{Type: NodePreference, Title: "preference"})
	store.SaveEmbedding(idPref, []float32{0.68, 0.32, 0.0}, "test-model")
	store.UpdateContentLength(idPref, 100)

	r := NewRetriever(RetrieverConfig{
		Store:     store,
		Content:   cm,
		Logger:    slog.Default(),
		TypeBoost: 1.2,
		TopK:      2,
	})
	mock.EmbedResponse = [][]float32{{0.70, 0.30, 0.0}}
	r.SetEmbedder(mock, "test-model")

	got, err := r.Retrieve(context.Background(), "", "test", nil)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(got.Nodes) < 2 {
		t.Fatalf("expected 2 nodes, got %d", len(got.Nodes))
	}
	if got.Nodes[0].Node.Type != NodePreference {
		t.Errorf("expected preference first (boosted), got %s", got.Nodes[0].Node.Type)
	}
}

func TestRetrieverNZTolkienScenario(t *testing.T) {
	dir := t.TempDir()
	db := testDB(t)
	store := NewStore(db)
	cm := NewContentManager(dir)
	mock := provider.NewMock()

	// Preference: Tolkien books with lateral triggers that connect to NZ.
	tolkienTriggers := []string{
		"Tolkien", "Lord of the Rings", "fantasy novels", "favorite authors",
		"book recommendations", "New Zealand filming locations", "Hobbiton",
		"adventure stories", "Middle-earth",
	}
	idTolkien, _ := store.CreateNode(&Node{
		Type:              NodePreference,
		Title:             "Enjoys Tolkien books",
		Summary:           "The user loves reading Tolkien novels, especially Lord of the Rings.",
		RetrievalTriggers: tolkienTriggers,
		EnrichmentStatus:  EnrichmentComplete,
	})
	path, _ := cm.Write(idTolkien, "The user is a big fan of J.R.R. Tolkien.")
	tolkienNode, _ := store.GetNode(idTolkien)
	tolkienNode.ContentPath = path
	store.UpdateNode(tolkienNode)
	store.UpdateContentLength(idTolkien, 40)

	// Preference: outdoor activities with triggers for adventure tourism.
	outdoorTriggers := []string{
		"outdoor activities", "hiking", "camping", "adventure tourism",
		"nature destinations", "hiking destinations", "trekking",
	}
	idOutdoor, _ := store.CreateNode(&Node{
		Type:              NodePreference,
		Title:             "Loves outdoor activities",
		Summary:           "The user enjoys hiking, camping, and outdoor adventures.",
		RetrievalTriggers: outdoorTriggers,
		EnrichmentStatus:  EnrichmentComplete,
	})
	path, _ = cm.Write(idOutdoor, "The user prefers outdoor and nature activities.")
	outdoorNode, _ := store.GetNode(idOutdoor)
	outdoorNode.ContentPath = path
	store.UpdateNode(outdoorNode)
	store.UpdateContentLength(idOutdoor, 47)

	// NZ travel query vector.
	queryVec := []float32{0.5, 0.3, 0.4, 0.2, 0.1}
	// Tolkien embedding: moderate similarity to NZ query.
	tolkienVec := []float32{0.4, 0.2, 0.5, 0.3, 0.1}
	// Outdoor embedding: moderate similarity.
	outdoorVec := []float32{0.5, 0.4, 0.3, 0.2, 0.2}

	store.SaveEmbedding(idTolkien, tolkienVec, "test-model")
	store.SaveEmbedding(idOutdoor, outdoorVec, "test-model")

	mock.EmbedResponse = [][]float32{queryVec}

	r := NewRetriever(RetrieverConfig{
		Store:         store,
		Content:       cm,
		Logger:        slog.Default(),
		TokenBudget:   2000,
		MinSimilarity: 0.2,
		TypeBoost:     1.1,
		TopK:          20,
	})
	r.SetEmbedder(mock, "test-model")

	got, err := r.Retrieve(context.Background(), "", "travel recommendations in New Zealand", nil)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	foundTolkien := false
	foundOutdoor := false
	for _, n := range got.Nodes {
		if n.Node.ID == idTolkien {
			foundTolkien = true
		}
		if n.Node.ID == idOutdoor {
			foundOutdoor = true
		}
	}
	if !foundTolkien {
		t.Error("Tolkien preference should appear in NZ travel results")
	}
	if !foundOutdoor {
		t.Error("outdoor activities preference should appear in NZ travel results")
	}
}
