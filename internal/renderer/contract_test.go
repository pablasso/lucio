package renderer

import (
	"strings"
	"testing"
)

func TestPrimitiveContractCoversV1NodeTypes(t *testing.T) {
	got := map[NodeType]bool{}
	for _, primitive := range PrimitiveList() {
		got[primitive.Type] = true
		if len(primitive.Required) == 0 {
			t.Fatalf("primitive %s has no required fields", primitive.Type)
		}
		if primitive.Fields["box"] == nil {
			t.Fatalf("primitive %s does not document box", primitive.Type)
		}
	}
	for _, typ := range []NodeType{NodeTypeText, NodeTypeRect, NodeTypeLine, NodeTypeImage, NodeTypeRule, NodeTypeCard, NodeTypeGrid, NodeTypeList, NodeTypeStack, NodeTypeSection} {
		if !got[typ] {
			t.Fatalf("missing primitive metadata for %s", typ)
		}
	}
}

func TestFontAliasesAreFixed(t *testing.T) {
	aliases := FontAliases()
	if len(aliases) != 3 {
		t.Fatalf("FontAliases len = %d, want 3", len(aliases))
	}
	want := []FontFamily{FontFamilySans, FontFamilySerif, FontFamilyMono}
	for i, family := range want {
		if aliases[i].Family != family {
			t.Fatalf("alias %d family = %s, want %s", i, aliases[i].Family, family)
		}
		if aliases[i].RegularFace == "" || aliases[i].BoldFace == "" {
			t.Fatalf("alias %s missing face names: %+v", family, aliases[i])
		}
	}
}

func TestDescribeMarkdownMentionsLineDecision(t *testing.T) {
	desc := DescribeMarkdown()
	for _, needle := range []string{"Lucio scene v1", "1440x2560", "Noto Sans", "`line`"} {
		if !strings.Contains(desc, needle) {
			t.Fatalf("DescribeMarkdown missing %q", needle)
		}
	}
}
