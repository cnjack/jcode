package modelcatalog

import (
	"reflect"
	"testing"
)

func flag(value bool) *bool { return &value }

func decodeOne(t *testing.T, body string) Entry {
	t.Helper()
	entries, err := Decode([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	return entries[0]
}

func TestDecodeCopilotCapabilities(t *testing.T) {
	t.Parallel()
	entries, err := Decode([]byte(`{"object":"list","data":[
		{"id":"claude-sonnet-5","object":"model","owned_by":"Anthropic","name":"Claude Sonnet 5",
		 "capabilities":{"type":"chat","family":"claude-sonnet-5",
		  "limits":{"vision":{"max_prompt_images":5},"max_output_tokens":64000,
		            "max_prompt_tokens":200000,"max_context_window_tokens":264000},
		  "supports":{"vision":true,"streaming":true,"tool_calls":true,
		              "reasoning_effort":["low","medium","high","xhigh","max"],"adaptive_thinking":true}}},
		{"id":"claude-haiku-4.5","name":"Claude Haiku 4.5","owned_by":"Anthropic",
		 "capabilities":{"type":"chat","limits":{"max_prompt_tokens":136000,"max_context_window_tokens":200000},
		  "supports":{"tool_calls":true,"max_thinking_budget":32000}}},
		{"id":"text-embedding-3-small","name":"Embedding V3 small","model_picker_enabled":false,
		 "capabilities":{"type":"embeddings","limits":{"max_inputs":512},"supports":{}}}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{
			ID: "claude-sonnet-5", Name: "Claude Sonnet 5", Vendor: "Anthropic", Kind: KindChat,
			Context: 200000, Attachment: flag(true), ToolCall: flag(true), Reasoning: flag(true),
			EffortTiers: []string{"low", "medium", "high", "xhigh", "max"},
		},
		{
			ID: "claude-haiku-4.5", Name: "Claude Haiku 4.5", Vendor: "Anthropic", Kind: KindChat,
			Context: 136000, Attachment: flag(false), ToolCall: flag(true), Reasoning: flag(false),
		},
		{
			ID: "text-embedding-3-small", Name: "Embedding V3 small", Kind: KindEmbedding,
			Attachment: flag(false), Reasoning: flag(false), Hidden: true,
		},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries =\n%#v\nwant\n%#v", entries, want)
	}
	if !entries[0].Selectable() || !entries[1].Selectable() || entries[2].Selectable() {
		t.Fatalf("selectable mismatch: %#v", entries)
	}
}

func TestDecodeOpenRouterShape(t *testing.T) {
	t.Parallel()
	got := decodeOne(t, `{"data":[{
		"id":"anthropic/claude-sonnet-4","name":"Anthropic: Claude Sonnet 4","context_length":1000000,
		"architecture":{"modality":"text+image->text","input_modalities":["text","image","file"],"output_modalities":["text"]},
		"top_provider":{"context_length":1000000,"max_completion_tokens":64000},
		"supported_parameters":["max_tokens","reasoning","include_reasoning","tools","tool_choice"]
	}]}`)
	want := Entry{
		ID: "anthropic/claude-sonnet-4", Name: "Anthropic: Claude Sonnet 4", Kind: KindChat,
		Context: 1000000, Attachment: flag(true), ToolCall: flag(true), Reasoning: flag(true),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entry = %#v, want %#v", got, want)
	}

	image := decodeOne(t, `[{"id":"img","architecture":{"modality":"text->image","output_modalities":["image"]},
		"supported_parameters":["seed"]}]`)
	if image.Kind != KindImage || image.Selectable() {
		t.Fatalf("image-only model should not be selectable: %#v", image)
	}
}

func TestDecodeFlagAndListDialects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want Entry
	}{
		{
			name: "plain openai",
			body: `{"data":[{"id":"gpt-4o","object":"model","owned_by":"system"}]}`,
			want: Entry{ID: "gpt-4o", Vendor: "system"},
		},
		{
			name: "mistral capabilities object",
			body: `{"data":[{"id":"pixtral-large","name":"pixtral-large","max_context_length":131072,
				"capabilities":{"completion_chat":true,"function_calling":true,"vision":true}}]}`,
			want: Entry{ID: "pixtral-large", Context: 131072, Attachment: flag(true), ToolCall: flag(true)},
		},
		{
			name: "lm studio list capabilities",
			body: `{"data":[{"id":"qwen3-vl","type":"vlm","max_context_length":32768,"capabilities":["tool_use"]}]}`,
			want: Entry{ID: "qwen3-vl", Kind: KindChat, Context: 32768, Attachment: flag(true), ToolCall: flag(true)},
		},
		{
			name: "ollama style embedding list",
			body: `{"models":[{"name":"nomic-embed-text","capabilities":["embedding"]}]}`,
			want: Entry{ID: "nomic-embed-text", Kind: KindEmbedding, Attachment: flag(false)},
		},
		{
			name: "vllm max_model_len",
			body: `{"data":[{"id":"Qwen/Qwen3-32B","owned_by":"vllm","max_model_len":40960}]}`,
			want: Entry{ID: "Qwen/Qwen3-32B", Vendor: "vllm", Context: 40960},
		},
		{
			name: "litellm style flags",
			body: `{"data":[{"id":"x","max_input_tokens":"128000","mode":"chat",
				"supports_vision":false,"supports_function_calling":true,"supports_reasoning":true}]}`,
			want: Entry{ID: "x", Kind: KindChat, Context: 128000, Attachment: flag(false), ToolCall: flag(true), Reasoning: flag(true)},
		},
		{
			name: "codex reasoning levels",
			body: `{"models":{"gpt-5.4":{"display_name":"GPT-5.4",
				"supported_reasoning_levels":[{"effort":"Low"},{"effort":"high"},{"effort":"low"}]}}}`,
			want: Entry{ID: "gpt-5.4", Name: "GPT-5.4", Reasoning: flag(true), EffortTiers: []string{"low", "high"}},
		},
		{
			name: "models.dev reasoning options",
			body: `{"data":[{"id":"r1","limit":{"context":65536},
				"reasoning_options":[{"type":"budget"},{"type":"effort","values":["low","high"]}]}]}`,
			want: Entry{ID: "r1", Context: 65536, Reasoning: flag(true), EffortTiers: []string{"low", "high"}},
		},
		{
			name: "explicitly no tools",
			body: `{"data":[{"id":"base","supports_tools":false}]}`,
			want: Entry{ID: "base", ToolCall: flag(false)},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := decodeOne(t, test.body); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("entry = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestParseKeepsOrderAndDropsDuplicates(t *testing.T) {
	t.Parallel()
	entries := Parse(map[string]any{"data": []any{
		"b", map[string]any{"id": "a"}, map[string]any{"id": "b", "name": "B2"}, map[string]any{}, 42,
	}})
	if len(entries) != 2 || entries[0].ID != "b" || entries[1].ID != "a" {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].Selectable() != true {
		t.Fatalf("id-only entry should be selectable: %#v", entries[0])
	}
}

func TestDecodeRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	if _, err := Decode([]byte("<html>")); err == nil {
		t.Fatal("expected decode error")
	}
}
