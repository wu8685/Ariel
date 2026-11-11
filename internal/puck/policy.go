package puck

import (
	"path/filepath"
	"slices"
	"strings"
)

var highRiskTags = map[string]struct{}{
	"push": {}, "deploy": {}, "release": {}, "delete": {}, "destructive": {}, "credential": {}, "payment": {}, "external_message": {},
}

var riskyPromptFragments = []string{"git push", "--force", "rm -rf", "kubectl apply", "deploy", "drop table"}

type PolicyInput struct {
	Tool, DeviceID, CWD, ThreadID, TurnOrigin, Prompt string
	PermissionMode                                    string
	RiskTags                                          []string
	AllowedScope                                      map[string][]string
	OwnedThread                                       bool
	ExecutionPolicyEnforceable                        bool
	AssistantWorkspaceRoot                            string
}

type PolicyDecision struct {
	RequiresAuthorization bool
	Rules                 []string
	DeniedCode            string
	DeclarationMismatch   bool
}

func EvaluatePolicy(input PolicyInput) PolicyDecision {
	if input.AssistantWorkspaceRoot != "" && pathWithin(input.AssistantWorkspaceRoot, input.CWD) {
		return PolicyDecision{DeniedCode: "PUCK_SELF_TARGET"}
	}
	decision := PolicyDecision{}
	// B2 protects other people's threads. Full Access on the brain does not waive it.
	if (input.Tool == "send_turn" || input.Tool == "interrupt") && !input.OwnedThread {
		decision.Rules = append(decision.Rules, "B2")
	}
	if input.PermissionMode == "full" {
		decision.RequiresAuthorization = len(decision.Rules) > 0
		return decision
	}
	roots, deviceAllowed := input.AllowedScope[input.DeviceID]
	pathAllowed := false
	for _, root := range roots {
		pathAllowed = pathAllowed || pathWithin(root, input.CWD)
	}
	if !deviceAllowed || input.CWD != "" && !pathAllowed {
		decision.Rules = append(decision.Rules, "B1")
	}
	if input.Tool == "interrupt" && input.TurnOrigin != "" && input.TurnOrigin != "puck" {
		decision.Rules = append(decision.Rules, "B3")
	}
	declaredRisk := false
	for _, tag := range input.RiskTags {
		if _, high := highRiskTags[strings.ToLower(tag)]; high {
			declaredRisk = true
			break
		}
	}
	if declaredRisk {
		decision.Rules = append(decision.Rules, "B5")
	}
	lowerPrompt := strings.ToLower(input.Prompt)
	contentRisk := false
	for _, fragment := range riskyPromptFragments {
		contentRisk = contentRisk || strings.Contains(lowerPrompt, fragment)
	}
	if contentRisk && !declaredRisk {
		decision.Rules = append(decision.Rules, "B6")
		decision.DeclarationMismatch = true
	}
	if !input.ExecutionPolicyEnforceable {
		decision.Rules = append(decision.Rules, "B10")
	}
	slices.Sort(decision.Rules)
	decision.Rules = slices.Compact(decision.Rules)
	decision.RequiresAuthorization = len(decision.Rules) > 0
	return decision
}

func pathWithin(root, target string) bool {
	if root == "" || target == "" || !filepath.IsAbs(root) || !filepath.IsAbs(target) {
		return false
	}
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func NativeInteractionRequiresUser(kind string) bool {
	switch kind {
	case "command_approval", "file_approval", "permission_request", "user_input", "unsupported":
		return true
	default:
		return false
	}
}
