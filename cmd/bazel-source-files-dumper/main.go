package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"strings"

	build "github.com/zoidbergwill/bazel-query-parser/blaze_query"
)

func GetLabel(target *build.Target) string {
	switch target.GetType() {
	case build.Target_RULE:
		return target.Rule.GetName()
	case build.Target_SOURCE_FILE:
		return target.SourceFile.GetName()
	case build.Target_GENERATED_FILE:
		return target.GeneratedFile.GetName()
	case build.Target_PACKAGE_GROUP:
		return target.PackageGroup.GetName()
	}
	log.Fatalf("Invalid target: %s", target.GetType())
	return ""
}

// # Run /bazel query --output=proto --order_output=no "//external:all-targets + deps(//...:all-targets)" > test_output.proto
type OutputTarget struct {
	Label string `json:"label"`
	// RuleClass string `json:"rule_class"`
	// Type      build.Target_Discriminator `json:"type"`
	// Digest    string
	Inputs []string `json:"inputs"`
}

type Output struct {
	Targets []OutputTarget `json:"targets"`
}

func main() {
	fn := "test_output.proto"
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		log.Fatalf("failed to read file %s: %s", fn, err)
	}
	queryOutput := build.QueryResult{}
	err = queryOutput.XXX_Unmarshal(data)
	if err != nil {
		log.Fatalf("failed to parse query result from %q: %s", fn, err)
	}
	output := Output{}
	for _, target := range queryOutput.Target {
		label := GetLabel(target)
		if target.GetType() != build.Target_RULE {
			// Skipping because not rule
			// fmt.Printf("Skipping %s: %s\n", label, target.GetType())
			continue
		}
		if strings.HasPrefix(label, "@") || strings.HasPrefix(label, "//external") {
			// Skipping because external
			continue
		}
		//fmt.Printf("%s: %s\n", label, target.GetType())
		var inputs []string
		for _, input := range target.GetRule().RuleInput {
			if strings.HasPrefix(input, "@") {
				continue
			}
			inputs = append(inputs, input)
		}
		output.Targets = append(output.Targets, OutputTarget{
			Label:  label,
			Inputs: inputs,
			// RuleClass: target.Rule.GetRuleClass(),
			// Type:      target.GetType(),
			// Digest:    digest,
		})
	}
	outputBytes, err := json.Marshal(output)
	if err != nil {
		log.Fatalf("failed to encode output as JSON: %s", err)
	}
	fmt.Printf("%s", outputBytes)
}
