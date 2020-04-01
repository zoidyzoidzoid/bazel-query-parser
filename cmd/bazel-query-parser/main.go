package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang/protobuf/proto"

	build "github.com/zoidbergwill/bazel-query-parser/blaze_query"
)

// # Run /bazel query --output=proto --order_output=full "//external:all-targets + deps(//...:all-targets)" > test_output.proto
//
// def get_label(target):
//     if target.type == 1:
//         return target.rule.name
//     if target.type == 2:
//         return target.source_file.name
//     if target.type == 3:
//         return target.generated_file.name
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
	// dafuq IntelliJ?
	return ""
}

// def create_target_map(targets):
//     target_map = {}
//     for target in targets:
//         label = get_label(target)
//         target_map[label] = target
//
//     return target_map
func CreateTargetMap(targets []*build.Target) map[string]*build.Target {
	result := make(map[string]*build.Target)
	for _, target := range targets {
		result[GetLabel(target)] = target
	}
	return result
}

// def get_rule_attributes(rule):
//     attributes = []
//     for a in rule.attribute:
//         if (a.name != "generator_location" and
//             a.name != "path" and
//                 a.name != "build_file"):
//             attributes.append(a)
//     return attributes

func GetRuleAttributes(rule build.Rule) []*build.Attribute {
	ignoredAttributes := map[string]bool{
		"build_file":         true,
		"generator_location": true,
		"path":               true,
	}

	var result []*build.Attribute
	for _, attribute := range rule.Attribute {
		if ignoredAttributes[attribute.GetName()] {
			continue
		}
		result = append(result, attribute)
	}
	return result
}

// def get_source_file_location(source_file):
//     path_to_target = os.path.dirname(source_file.location.split(":")[0])
//     path_from_target = source_file.name.replace("//", '').split(":")[-1]
//     combined_path = os.path.join(path_to_target, path_from_target)
//
//     if not os.path.exists(combined_path):
//         print("File %s does not exist on disk" % combined_path)
//         print(source_file.location)
//         print(source_file.name)
//         return ""
//
//     return combined_path
var cachedSourceFileLocations map[string]*string

func GetSourceFileLocation(source build.SourceFile) string {
	if cachedSourceFileLocations == nil {
		cachedSourceFileLocations = make(map[string]*string)
	}

	pathToTarget := filepath.Dir(strings.Split(source.GetLocation(), ":")[0])

	pathFromTargetChunks := strings.Split(strings.Replace(source.GetName(), "//", "", 1), ":")
	pathFromTarget := pathFromTargetChunks[len(pathFromTargetChunks)-1]

	combinedPath := filepath.Join(pathToTarget, pathFromTarget)

	value := cachedSourceFileLocations[combinedPath]
	if value != nil {
		return *value
	}

	_, err := os.Stat(combinedPath)
	if err != nil {
		if os.IsNotExist(err) {
			 log.Printf("file %s does not exist on disk\n%s\n%s", combinedPath, source.GetLocation(), source.GetName())
		} else {
			log.Printf("failed to read file %q: %s", combinedPath, err)
		}
		value := ""
		cachedSourceFileLocations[combinedPath] = &value
		return value
	}

	cachedSourceFileLocations[combinedPath] = &combinedPath
	return combinedPath
}

// def calculate_hash_for_rule(rule, target_map):
//     sha256 = hashlib.sha256()
//     for a in get_rule_attributes(rule):
//         sha256.update(a.SerializeToString())
//
//     for i in rule.rule_input:
//         input_target = target_map.get(i)
//         if input_target is None:
//             print("%s not found in target map" % i)
//         else:
//             sha256.update(calculate_hash(i, target_map[i], target_map))
//
//     return sha256.digest()
func CalculateHashForRule(rule build.Rule, targetMap map[string]*build.Target) []byte {
	h := sha256.New()
	for _, attribute := range GetRuleAttributes(rule) {
		err := proto.CompactText(h, attribute)
		if err != nil {
			log.Fatalf("Failed to add hash for rule %s: %s", attribute, err)
		}
	}
	for _, i := range rule.RuleInput {
		inputTarget := targetMap[i]
		if inputTarget == nil {
			log.Printf("%s not found in target map", i)
		} else {
			h.Write(CalculateHash(i, targetMap[i], targetMap))
		}
	}
	return h.Sum(nil)
}

// def calculate_hash_for_source_file(source_file):
//     sha256 = hashlib.sha256()
//     file_location = get_source_file_location(source_file)
//     if file_location == "":
//         return sha256.digest()
//
//     with open(file_location, "rb") as f:
//         file_bytes = f.read()  # read entire file as bytes
//         sha256.update(file_bytes)
//     return sha256.digest()
func CalculateHashForSourceFile(source build.SourceFile) []byte {
	h := sha256.New()
	fileLocation := GetSourceFileLocation(source)
	if fileLocation == "" {
		return h.Sum(nil)
	}

	data, err := ioutil.ReadFile(fileLocation)
	if err != nil {
		log.Printf("Failed to read file %s: %s", fileLocation, err)
	}
	h.Write(data)
	return h.Sum(nil)
}

// hashed_targets = {}
//
//
// def calculate_hash(label, target, target_map):
//     cached_target = hashed_targets.get(label)
//
//     if cached_target is not None:
//         return cached_target
//
//     sha256 = hashlib.sha256()
//     if target.type == 1:
//         sha256.update(calculate_hash_for_rule(target.rule, target_map))
//     if target.type == 2:
//         sha256.update(calculate_hash_for_source_file(target.source_file))
//     if target.type == 3:
//         sha256.update(
//             calculate_hash(label,
//                            target_map[target.generated_file.generating_rule],
//                            target_map))
//
//     digest = sha256.digest()
//     hashed_targets[label] = digest
//     return digest
var hashedTargets map[*string]*[]byte

func CalculateHash(label string, target *build.Target, targetMap map[string]*build.Target) []byte {
	if hashedTargets == nil {
		hashedTargets = make(map[*string]*[]byte)
	}
	cachedTarget := hashedTargets[&label]

	if cachedTarget != nil {
		return *cachedTarget
	}

	h := sha256.New()
	switch *target.Type {
	case build.Target_RULE:
		h.Write(CalculateHashForRule(*target.GetRule(), targetMap))
	case build.Target_SOURCE_FILE:
		h.Write(CalculateHashForSourceFile(*target.GetSourceFile()))
	case build.Target_GENERATED_FILE:
		h.Write(CalculateHash(label, targetMap[target.GeneratedFile.GetGeneratingRule()], targetMap))
	//case build.Target_PACKAGE_GROUP:
	//	h.Write(CalculateHashForPackageGroup(*target.GetPackageGroup(), targetMap))
	default:
		log.Printf("Skipped target: %s", label)
	}
	digest := h.Sum(nil)
	hashedTargets[&label] = &digest
	return digest
}

//// New function
//func CalculateHashForPackageGroup(group build.PackageGroup, targetMap map[string]*build.Target) []byte {
//	log.Fatalf("Found package group: %s", group)
//	h := sha256.New()
//	//fileLocation := GetSourceFileLocation(source)
//	//if fileLocation == "" {
//	//	return h.Sum(nil)
//	//}
//	//
//	//data, err := ioutil.ReadFile(fileLocation)
//	//if err != nil {
//	//	log.Printf("Failed to read file %s: %s", fileLocation, err)
//	//}
//	//h.Write(data)
//	return h.Sum(nil)
//}

// # TODO: External workspaces(https://www.youtube.com/watch?v=9Dk7mtIm7_A&list=PLxNYxgaZ8Rsf-7g43Z8LyXct9ax6egdSj&index=9&t=1582s)
// with open("test_output.proto", "rb") as f:
//     query_output = query_pb2.QueryResult()
//     query_output.ParseFromString(f.read())
//
//     target_map = create_target_map(query_output.target)
//     output = {
//         "targets": []
//     }
//     for label, target in target_map.items():
//         if target.type == 1:
//             hash_bytes = calculate_hash(label, target, target_map)
//
//             digest = binascii.hexlify(bytearray(hash_bytes))
//
//             output["targets"].append({
//                 "label": label,
//                 "rule_class": target.rule.rule_class,
//                 "digest": str(digest, "ascii")
//             })
//
//     print(json.dumps(output))
type OutputTarget struct {
	Label     string
	RuleClass string
	Digest    string
}

type Output struct {
	Targets []OutputTarget
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
	targetMap := CreateTargetMap(queryOutput.Target)
	output := Output{}
	for label, target := range targetMap {
		if target.GetType() != build.Target_RULE {
			log.Printf("skipping target of wrong type %s: %s", target.GetType(), label)
			continue
		}
		hashBytes := CalculateHash(label, target, targetMap)
		digest := hex.EncodeToString(hashBytes)
		output.Targets = append(output.Targets, OutputTarget{
			Label:     label,
			RuleClass: target.Rule.GetRuleClass(),
			Digest:    digest,
		})
	}
	outputBytes, err := json.Marshal(output)
	if err != nil {
		log.Fatalf("failed to encode output as JSON: %s", err)
	}
	fmt.Printf("%s", outputBytes)
}
