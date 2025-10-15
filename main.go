package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// --- Custom Error ---

// AssemblerError is a custom error type for assembler-specific errors.
type AssemblerError struct {
	Message string
}

func (e *AssemblerError) Error() string {
	return e.Message
}

// --- Data Structures ---

// MicrocontrollerConfig holds all configuration details for a specific microcontroller.
type MicrocontrollerConfig struct {
	ProgramMemorySize   int                        `json:"PROGRAM_MEMORY_SIZE"`
	TotalMemoryBytes    int                        `json:"TOTAL_MEMORY_BYTES"`
	InstructionSet      map[string]InstructionInfo `json:"INSTRUCTION_SET"`
	SFRMap              map[string]int             `json:"SFR_MAP"`
	AllConfigFuseMaps   []map[string]FuseGroupInfo `json:"ALL_CONFIG_FUSE_MAPS"`
	ConfigWordDefaults  map[string]ConfigDefault   `json:"CONFIG_WORD_DEFAULTS"`
	ProgramWordSizeBits int                        `json:"PROGRAM_WORD_SIZE_BITS"`
}

// InstructionInfo defines the structure for an instruction.
type InstructionInfo struct {
	OpcodePattern string   `json:"opcode_pattern"`
	Operands      []string `json:"operands"`
}

// FuseGroupInfo defines the structure for a fuse group.
type FuseGroupInfo struct {
	Mask   int            `json:"mask"`
	Values map[string]int `json:"values"`
}

// ConfigDefault defines the structure for a config word default.
type ConfigDefault struct {
	DefaultValue int `json:"default_value"`
	Address      int `json:"address"`
	Padding      int `json:"padding"`
}

// AssemblyItem is an interface representing any line item in parsed assembly code.
type AssemblyItem interface {
	isAssemblyItem()
}

// ExpandedParsedAssembly holds the final, macro-expanded list of assembly items.
type ExpandedParsedAssembly struct {
	Lines []AssemblyItem
}

// ParsedAssembly holds the result of the initial parsing pass.
type ParsedAssembly struct {
	Lines   []AssemblyItem
	Defines map[string]string
	Macros  map[string]*MacroDefinition
	Labels  map[string]int
	Symbols map[string]string
}

// Define structs for each assembly item type.
// They all implement the AssemblyItem interface via the dummy method.

type Comment struct {
	Text string
}

func (c *Comment) isAssemblyItem() {}

type Define struct {
	Name  string
	Value string
}

func (d *Define) isAssemblyItem() {}

type Instruction struct {
	Opcode   string
	Operands []string
	Comment  string
}

func (i *Instruction) isAssemblyItem() {}

type OrgDirective struct {
	Address string
	Comment string
}

func (o *OrgDirective) isAssemblyItem() {}

type EquDirective struct {
	Symbol  string
	Value   string
	Comment string
}

func (e *EquDirective) isAssemblyItem() {}

type ConfigDirective struct {
	Options []string
	Comment string
}

func (c *ConfigDirective) isAssemblyItem() {}

type Label struct {
	Name    string
	Comment string
}

func (l *Label) isAssemblyItem() {}

type MacroDefinition struct {
	Name         string
	Body         []AssemblyItem
	MacroComment string
}

func (m *MacroDefinition) isAssemblyItem() {}

// --- ASM Parser ---

// ASMParser parses assembly files.
type ASMParser struct {
	parsedData              *ParsedAssembly
	expandedParsedData      *ExpandedParsedAssembly
	currentSourceLineNumber int
	relabelCounters         map[string]int
	currentMacroLabelsMap   map[string]string
}

// NewASMParser creates a new parser instance.
func NewASMParser() *ASMParser {
	return &ASMParser{
		parsedData: &ParsedAssembly{
			Lines:   make([]AssemblyItem, 0),
			Defines: make(map[string]string),
			Macros:  make(map[string]*MacroDefinition),
			Labels:  make(map[string]int),
			Symbols: make(map[string]string),
		},
		expandedParsedData:    &ExpandedParsedAssembly{Lines: make([]AssemblyItem, 0)},
		relabelCounters:       make(map[string]int),
		currentMacroLabelsMap: make(map[string]string),
	}
}

// extractLineContentAndComment separates the main content of a line from its comment.
func (p *ASMParser) extractLineContentAndComment(line string) (string, string) {
	parts := strings.SplitN(line, ";", 2)
	content := strings.TrimSpace(parts[0])
	comment := ""
	if len(parts) > 1 {
		comment = ";" + parts[1]
	}
	return content, strings.TrimSpace(comment)
}

// generateUniqueLabelName creates a unique label name for use within macros.
func (p *ASMParser) generateUniqueLabelName(originalLabelName string) string {
	counter, exists := p.relabelCounters[originalLabelName]
	if !exists {
		p.relabelCounters[originalLabelName] = -1
	}
	p.relabelCounters[originalLabelName]++
	counter = p.relabelCounters[originalLabelName]

	newName := fmt.Sprintf("%s_M%d", originalLabelName, counter)

	// Ensure absolute uniqueness
	for {
		if _, exists := p.parsedData.Labels[newName]; !exists {
			break
		}
		p.relabelCounters[originalLabelName]++
		counter = p.relabelCounters[originalLabelName]
		newName = fmt.Sprintf("%s_M%d", originalLabelName, counter)
	}
	return newName
}

// substituteOperand recursively substitutes an operand if it's a #DEFINE.
func (p *ASMParser) substituteOperand(operand string) string {
	visited := make(map[string]struct{})
	currentValue := operand
	for {
		val, exists := p.parsedData.Defines[currentValue]
		if !exists {
			break
		}
		if _, seen := visited[currentValue]; seen {
			break // Prevent infinite recursion
		}
		visited[currentValue] = struct{}{}
		currentValue = val
	}
	return currentValue
}

// Compile regexes once for efficiency
var (
	defineRegex      = regexp.MustCompile(`(?i)^#DEFINE\s+([A-Z_0-9]+)\s+(.*)$`)
	configRegex      = regexp.MustCompile(`(?i)^__CONFIG\s+(.*)$`)
	orgRegex         = regexp.MustCompile(`(?i)^ORG\s+(0[Xx][0-9a-fA-F]+|[0-9]+)$`)
	equRegex         = regexp.MustCompile(`(?i)^([A-Z_0-9]+)\s+EQU\s+(0[Xx][0-9a-fA-F]+|[0-9]+)$`)
	labelRegex       = regexp.MustCompile(`(?i)^([A-Z_0-9]+):$`)
	instructionRegex = regexp.MustCompile(`(?i)^([A-Z_0-9]+)\s*(.*)$`)
	macroStartRegex  = regexp.MustCompile(`(?i)^([A-Z_0-9]+)\s+MACRO\s*(;.*)?$`)
)

// parseSingleLineItem parses one line of assembly code.
func (p *ASMParser) parseSingleLineItem(line string, inMacroContext bool) (AssemblyItem, error) {
	originalLine := line
	lineContent, commentText := p.extractLineContentAndComment(line)

	if lineContent == "" && commentText == "" {
		return nil, nil // Skip empty lines
	}

	if strings.HasPrefix(strings.TrimSpace(originalLine), ";") {
		return &Comment{Text: strings.TrimSpace(originalLine)}, nil
	}

	if match := defineRegex.FindStringSubmatch(lineContent); match != nil {
		name, value := match[1], strings.TrimSpace(match[2])
		p.parsedData.Defines[name] = value
		return &Define{Name: name, Value: value}, nil
	}

	if match := configRegex.FindStringSubmatch(lineContent); match != nil {
		optionsStr := strings.TrimSpace(match[1])
		options := strings.Split(optionsStr, "&")
		for i := range options {
			options[i] = strings.TrimSpace(options[i])
		}
		return &ConfigDirective{Options: options, Comment: commentText}, nil
	}

	if match := orgRegex.FindStringSubmatch(lineContent); match != nil {
		return &OrgDirective{Address: match[1], Comment: commentText}, nil
	}

	if match := equRegex.FindStringSubmatch(lineContent); match != nil {
		symbol, value := match[1], match[2]
		p.parsedData.Symbols[symbol] = value
		return &EquDirective{Symbol: symbol, Value: value, Comment: commentText}, nil
	}

	if match := labelRegex.FindStringSubmatch(lineContent); match != nil {
		originalLabelName := match[1]
		finalLabelName := originalLabelName
		if inMacroContext {
			finalLabelName = p.generateUniqueLabelName(originalLabelName)
			p.currentMacroLabelsMap[originalLabelName] = finalLabelName
		}
		p.parsedData.Labels[finalLabelName] = p.currentSourceLineNumber
		return &Label{Name: finalLabelName, Comment: commentText}, nil
	}

	if match := instructionRegex.FindStringSubmatch(lineContent); match != nil {
		opcode := match[1]
		operandsStr := strings.TrimSpace(match[2])

		// Split by comma then by space
		var operands []string
		parts := strings.Split(operandsStr, ",")
		for _, part := range parts {
			subParts := strings.Fields(part)
			operands = append(operands, subParts...)
		}

		// Substitute #DEFINEs
		for i, op := range operands {
			operands[i] = p.substituteOperand(op)
		}

		// Re-label operands if in macro
		if inMacroContext {
			for i, op := range operands {
				if newLabel, ok := p.currentMacroLabelsMap[op]; ok {
					operands[i] = newLabel
				}
			}
		}
		return &Instruction{Opcode: opcode, Operands: operands, Comment: commentText}, nil
	}

	fmt.Printf("Warning: Unhandled line type at source line %d: '%s'\n", p.currentSourceLineNumber, originalLine)
	return nil, nil
}

// Parse processes the entire assembly content string.
func (p *ASMParser) Parse(asmContent string) (*ParsedAssembly, error) {
	lines := strings.Split(asmContent, "\n")
	inMacro := false
	var currentMacroName string
	var macroBodyLines []string
	var macroStartComment string

	for i, line := range lines {
		p.currentSourceLineNumber = i + 1
		strippedLine := strings.TrimSpace(line)

		if match := macroStartRegex.FindStringSubmatch(strippedLine); match != nil && !inMacro {
			currentMacroName = match[1]
			inMacro = true
			macroBodyLines = []string{}
			macroStartComment = ""
			if len(match) > 2 {
				macroStartComment = match[2]
			}
			p.currentMacroLabelsMap = make(map[string]string)
			continue
		}

		if strings.ToUpper(strippedLine) == "ENDM" && inMacro {
			inMacro = false
			var parsedMacroBody []AssemblyItem
			for _, macroLine := range macroBodyLines {
				parsedItem, err := p.parseSingleLineItem(macroLine, true)
				if err != nil {
					return nil, err
				}
				if parsedItem != nil {
					parsedMacroBody = append(parsedMacroBody, parsedItem)
				}
			}

			macroDef := &MacroDefinition{
				Name:         currentMacroName,
				Body:         parsedMacroBody,
				MacroComment: macroStartComment,
			}
			p.parsedData.Macros[currentMacroName] = macroDef
			p.parsedData.Lines = append(p.parsedData.Lines, macroDef)

			// Reset state
			currentMacroName = ""
			macroBodyLines = []string{}
			p.currentMacroLabelsMap = make(map[string]string)
			continue
		}

		if inMacro {
			macroBodyLines = append(macroBodyLines, line)
		} else {
			parsedItem, err := p.parseSingleLineItem(line, false)
			if err != nil {
				return nil, err
			}
			if parsedItem != nil {
				p.parsedData.Lines = append(p.parsedData.Lines, parsedItem)
			}
		}
	}
	return p.parsedData, nil
}

// ExpandMacros expands all macro invocations.
func (p *ASMParser) ExpandMacros(parsedAssembly *ParsedAssembly) (*ExpandedParsedAssembly, error) {
	for _, item := range parsedAssembly.Lines {
		switch v := item.(type) {
		case *Instruction:
			// Expand macro
			if macroToExpand, ok := p.parsedData.Macros[v.Opcode]; ok {
				p.expandedParsedData.Lines = append(p.expandedParsedData.Lines, &Comment{Text: fmt.Sprintf("; --- Expanding Macro: %s ---", v.Opcode)})
				p.expandedParsedData.Lines = append(p.expandedParsedData.Lines, macroToExpand.Body...)
				p.expandedParsedData.Lines = append(p.expandedParsedData.Lines, &Comment{Text: fmt.Sprintf("; --- End of Macro: %s ---", v.Opcode)})
				// Expand define used as instruction
			} else if defineValue, ok := p.parsedData.Defines[v.Opcode]; ok {
				newInstruction, err := p.parseSingleLineItem(defineValue, false)
				if err != nil {
					return nil, err
				}
				if newInstruction != nil {
					p.expandedParsedData.Lines = append(p.expandedParsedData.Lines, &Comment{Text: fmt.Sprintf("; --- Expanding Define: %s ---", v.Opcode)})
					p.expandedParsedData.Lines = append(p.expandedParsedData.Lines, newInstruction)
				}
			} else {
				p.expandedParsedData.Lines = append(p.expandedParsedData.Lines, v)
			}
		case *MacroDefinition, *Define:
			// Do not include definitions in the final output
		default:
			p.expandedParsedData.Lines = append(p.expandedParsedData.Lines, v)
		}
	}
	return p.expandedParsedData, nil
}

// --- Pic Assembler ---

type PicAssembler struct {
	mcConfig         *MicrocontrollerConfig
	parsedAssembly   *ExpandedParsedAssembly
	symbolTable      map[string]int
	configDirectives []struct {
		lineNum int
		options []string
	}
	machineCodeWords map[int]int
	configWords      map[string]int
	labels           map[string]int
}

// NewPicAssembler creates a new assembler instance.
func NewPicAssembler(mcConfig *MicrocontrollerConfig, parsedAssembly *ExpandedParsedAssembly) *PicAssembler {
	a := &PicAssembler{
		mcConfig:         mcConfig,
		parsedAssembly:   parsedAssembly,
		symbolTable:      make(map[string]int),
		machineCodeWords: make(map[int]int),
		configWords:      make(map[string]int),
		labels:           make(map[string]int),
	}
	// Initialize config words with defaults
	for name, info := range mcConfig.ConfigWordDefaults {
		a.configWords[name] = info.DefaultValue
	}
	return a
}

// evaluateExpression evaluates a numeric expression from a string.
func (a *PicAssembler) evaluateExpression(expression string) (int, error) {
	expression = strings.TrimSpace(expression)

	// Hex
	if strings.HasPrefix(expression, "0x") || strings.HasPrefix(expression, "0X") {
		val, err := strconv.ParseInt(expression[2:], 16, 64)
		return int(val), err
	}
	if strings.HasPrefix(expression, "$") {
		val, err := strconv.ParseInt(expression[1:], 16, 64)
		return int(val), err
	}
	// Binary
	if strings.HasPrefix(expression, "0b") || strings.HasPrefix(expression, "%") {
		val, err := strconv.ParseInt(expression[2:], 2, 64)
		return int(val), err
	}
	// Decimal
	if val, err := strconv.ParseInt(expression, 10, 64); err == nil {
		return int(val), nil
	}
	// Symbol Table
	if val, ok := a.symbolTable[expression]; ok {
		return val, nil
	}
	// SFR Map
	if val, ok := a.mcConfig.SFRMap[strings.ToUpper(expression)]; ok {
		return val, nil
	}

	return 0, &AssemblerError{Message: fmt.Sprintf("Undefined symbol or invalid expression: '%s'", expression)}
}

// firstPass builds the symbol table.
func (a *PicAssembler) firstPass() error {
	programCounter := 0
	a.labels = make(map[string]int)

	for i, item := range a.parsedAssembly.Lines {
		lineNum := i + 1

		switch v := item.(type) {
		case *EquDirective:
			if v.Symbol == "" {
				return &AssemblerError{Message: fmt.Sprintf("Line %d: EQU directive must have a label.", lineNum)}
			}
			val, err := a.evaluateExpression(v.Value)
			if err != nil {
				return &AssemblerError{Message: fmt.Sprintf("Line %d: Invalid EQU expression - %v", lineNum, err)}
			}
			a.symbolTable[v.Symbol] = val

		case *Label:
			if _, exists := a.symbolTable[v.Name]; exists {
				if _, isSFR := a.mcConfig.SFRMap[v.Name]; !isSFR {
					return &AssemblerError{Message: fmt.Sprintf("Line %d: Duplicate label '%s'", lineNum, v.Name)}
				}
			}
			a.symbolTable[v.Name] = programCounter
			a.labels[v.Name] = programCounter

		case *OrgDirective:
			var err error
			programCounter, err = a.evaluateExpression(v.Address)
			if err != nil {
				return &AssemblerError{Message: fmt.Sprintf("Line %d: Invalid ORG address - %v", lineNum, err)}
			}
			if programCounter < 0 || programCounter >= a.mcConfig.ProgramMemorySize {
				return &AssemblerError{Message: fmt.Sprintf("Line %d: ORG address 0x%X out of range.", lineNum, programCounter)}
			}

		case *ConfigDirective:
			a.configDirectives = append(a.configDirectives, struct {
				lineNum int
				options []string
			}{lineNum, v.Options})

		case *Instruction:
			if strings.ToUpper(v.Opcode) == "END" {
				goto endFirstPass // Exit loop on END directive
			}
			if _, ok := a.mcConfig.InstructionSet[strings.ToUpper(v.Opcode)]; ok {
				programCounter++
			}
		}
	}
endFirstPass:
	return nil
}

// secondPass generates machine code.
func (a *PicAssembler) secondPass() error {
	// Process Config Directives first
	for _, cd := range a.configDirectives {
		for _, setting := range cd.options {
			setting = strings.ToUpper(strings.TrimSpace(setting))
			foundSetting := false
			for i, configMap := range a.mcConfig.AllConfigFuseMaps {
				for _, groupInfo := range configMap {
					if value, ok := groupInfo.Values[setting]; ok {
						// Determine the config word name based on the index of the map.
						var configWordName string
						if i == 0 {
							configWordName = "CONFIG1"
						} else if i == 1 {
							configWordName = "CONFIG2"
						} else {
							// This handles PICs with more than 2 config words if defined (like PIC16F886).
							fmt.Printf("WARNING: Line %d: Fuse setting '%s' belongs to unmapped config word index %d. Skipping.\n", cd.lineNum, setting, i)
							continue
						}

						mask := groupInfo.Mask
						a.configWords[configWordName] &= ^mask
						a.configWords[configWordName] |= value
						foundSetting = true
						break
					}
				}
				if foundSetting {
					break
				}
			}
			if !foundSetting {
				fmt.Printf("WARNING: Line %d: Unknown fuse setting '%s'. Ignoring.\n", cd.lineNum, setting)
			}
		}
	}

	programCounter := 0
	for i, item := range a.parsedAssembly.Lines {
		lineNum := i + 1

		switch v := item.(type) {
		case *OrgDirective:
			var err error
			programCounter, err = a.evaluateExpression(v.Address)
			if err != nil {
				return err
			}

		case *Instruction:
			instruction := strings.ToUpper(v.Opcode)
			operands := v.Operands

			if instruction == "END" {
				return nil
			}

			instInfo, ok := a.mcConfig.InstructionSet[instruction]
			if !ok {
				return &AssemblerError{Message: fmt.Sprintf("Line %d: Unknown instruction or directive '%s'.", lineNum, instruction)}
			}

			if len(operands) != len(instInfo.Operands) {
				return &AssemblerError{Message: fmt.Sprintf("Line %d: Instruction '%s' expects %d operand(s), got %d.", lineNum, instruction, len(instInfo.Operands), len(operands))}
			}

			opcodePattern := instInfo.OpcodePattern
			machineWordChars := []rune(opcodePattern)

			operandValues := make(map[string]int)

			for opIdx, opType := range instInfo.Operands {
				opValueStr := operands[opIdx]
				if opType == "d" {
					switch strings.ToUpper(opValueStr) {
					case "W":
						operandValues["d"] = 0
					case "F":
						operandValues["d"] = 1
					default:
						return &AssemblerError{Message: fmt.Sprintf("Line %d: Invalid destination '%s'. Must be 'W' or 'F'.", lineNum, opValueStr)}
					}
				} else {
					val, err := a.evaluateExpression(opValueStr)
					if err != nil {
						return &AssemblerError{Message: fmt.Sprintf("Line %d: Invalid operand '%s' for '%s' - %v", lineNum, opValueStr, instruction, err)}
					}
					operandValues[opType] = val
				}
			}

			// Helper function to replace placeholders in the binary string
			replacePlaceholder := func(placeholder rune, value int, bits int) {
				binVal := fmt.Sprintf("%0*b", bits, value)
				if len(binVal) > bits {
					binVal = binVal[len(binVal)-bits:] // Truncate if larger
				}
				startIdx := strings.IndexRune(opcodePattern, placeholder)
				if startIdx == -1 {
					return
				}
				for j, char := range binVal {
					if startIdx+j < len(machineWordChars) {
						machineWordChars[startIdx+j] = char
					}
				}
			}

			if val, ok := operandValues["k11"]; ok {
				replacePlaceholder('k', val, 11)
			}
			if val, ok := operandValues["k8"]; ok {
				replacePlaceholder('L', val, 8)
			}
			if val, ok := operandValues["f"]; ok {
				// The file register address is split into 7 bits for the opcode and 2 for bank selection.
				// For this instruction set, only the lower 7 bits go into the opcode directly.
				replacePlaceholder('f', val&0x7F, 7)
				// TO DO: Handle RP0/RP1 bits in STATUS for banking. This implementation assumes user manages banking.
			}
			if val, ok := operandValues["b"]; ok {
				replacePlaceholder('b', val, 3)
			}
			if val, ok := operandValues["d"]; ok {
				replacePlaceholder('d', val, 1)
			}

			finalBinaryStr := strings.ReplaceAll(string(machineWordChars), "x", "0")

			if len(finalBinaryStr) != a.mcConfig.ProgramWordSizeBits {
				return &AssemblerError{Message: fmt.Sprintf("Line %d: Internal error: Generated binary string length mismatch for '%s'.", lineNum, instruction)}
			}

			parsedWord, err := strconv.ParseInt(finalBinaryStr, 2, 64)
			if err != nil {
				return &AssemblerError{Message: fmt.Sprintf("Line %d: Internal error converting binary string '%s' to integer.", lineNum, finalBinaryStr)}
			}

			a.machineCodeWords[programCounter] = int(parsedWord)
			programCounter++
		}
	}

	return nil
}

// GenerateReport creates a formatted string report of the assembly process.
func (a *PicAssembler) GenerateReport(rawText string) string {
	var report strings.Builder
	separator := strings.Repeat("=", 80)

	center := func(s string) string {
		pad := (80 - len(s)) / 2
		return strings.Repeat(" ", pad) + s
	}

	report.WriteString(center("Assembly Process Report") + "\n")

	// Original Code
	report.WriteString("\n" + separator + "\n")
	report.WriteString(center("Original Assembly Code") + "\n")
	report.WriteString(separator + "\n")
	for i, line := range strings.Split(rawText, "\n") {
		report.WriteString(fmt.Sprintf("%4d: %s\n", i+1, line))
	}

	// Labels
	report.WriteString("\n" + separator + "\n")
	report.WriteString(center("Labels (Symbol Table)") + "\n")
	report.WriteString(separator + "\n")
	if len(a.labels) > 0 {
		// Sort labels by name for consistent output
		sortedLabels := make([]string, 0, len(a.labels))
		for label := range a.labels {
			sortedLabels = append(sortedLabels, label)
		}
		sort.Strings(sortedLabels)
		for _, label := range sortedLabels {
			address := a.labels[label]
			report.WriteString(fmt.Sprintf("  %-20s -> 0x%04X\n", label, address))
		}
	} else {
		report.WriteString("  No labels found.\n")
	}

	// Config Words
	report.WriteString("\n" + separator + "\n")
	report.WriteString(center("Configuration Words") + "\n")
	report.WriteString(separator + "\n")
	if len(a.configWords) > 0 {
		for name, value := range a.configWords {
			report.WriteString(fmt.Sprintf("  %-20s = 0x%04X\n", name, value))
		}
	} else {
		report.WriteString("  No configuration words set.\n")
	}

	// Machine Code
	report.WriteString("\n" + separator + "\n")
	report.WriteString(center("Generated Machine Code") + "\n")
	report.WriteString(separator + "\n")
	if len(a.machineCodeWords) > 0 {
		// Sort addresses for ordered output
		addresses := make([]int, 0, len(a.machineCodeWords))
		for addr := range a.machineCodeWords {
			addresses = append(addresses, addr)
		}
		sort.Ints(addresses)
		for _, addr := range addresses {
			word := a.machineCodeWords[addr]
			report.WriteString(fmt.Sprintf("  0x%04X: 0x%04X\n", addr, word))
		}
	} else {
		report.WriteString("  No machine code generated.\n")
	}

	return report.String()
}

// --- Intel HEX File Generation ---

// calculateChecksum computes the 8-bit two's complement checksum.
func calculateChecksum(recordBytes []byte) byte {
	var sum byte
	for _, b := range recordBytes {
		sum += b
	}
	return -sum
}

// HexGenerator creates Intel HEX files.
type HexGenerator struct {
	mcConfig *MicrocontrollerConfig
}

// NewHexGenerator creates a new HEX generator.
func NewHexGenerator(mcConfig *MicrocontrollerConfig) *HexGenerator {
	return &HexGenerator{mcConfig: mcConfig}
}

// GenerateHex produces the Intel HEX file content as a string.
func (g *HexGenerator) GenerateHex(machineCodeWords map[int]int, configWords map[string]int) (string, error) {
	var hexLines strings.Builder
	const recordSize = 16 // Bytes per data record

	// --- Part 1: Process Program Memory ---
	fullMemoryBytes := make([]byte, g.mcConfig.TotalMemoryBytes)
	for i := range fullMemoryBytes {
		fullMemoryBytes[i] = 0xFF // Erased state
	}

	for wordAddr, word := range machineCodeWords {
		byteAddr := wordAddr * 2
		if byteAddr+1 < g.mcConfig.TotalMemoryBytes {
			mask := (1 << g.mcConfig.ProgramWordSizeBits) - 1
			value16bit := word & mask
			lowByte := byte(value16bit & 0xFF)
			highByte := byte((value16bit >> 8) & 0xFF)
			fullMemoryBytes[byteAddr] = lowByte
			fullMemoryBytes[byteAddr+1] = highByte
		} else {
			fmt.Printf("WARNING: Program memory address 0x%X out of bounds.\n", wordAddr)
		}
	}

	// ELA Record for address 0x0000
	hexLines.WriteString(":020000040000FA\n")

	endOfProgramMemory := g.mcConfig.ProgramMemorySize * 2
	for currentByteAddr := 0; currentByteAddr < endOfProgramMemory; currentByteAddr += recordSize {
		endOfChunk := currentByteAddr + recordSize
		if endOfChunk > endOfProgramMemory {
			endOfChunk = endOfProgramMemory
		}
		dataChunk := fullMemoryBytes[currentByteAddr:endOfChunk]

		// Skip if chunk is all 0xFF
		isErased := true
		for _, b := range dataChunk {
			if b != 0xFF {
				isErased = false
				break
			}
		}
		if isErased {
			continue
		}

		byteCount := len(dataChunk)
		addrField := currentByteAddr & 0xFFFF
		recordType := 0x00

		recordBytes := []byte{byte(byteCount), byte(addrField >> 8), byte(addrField), byte(recordType)}
		recordBytes = append(recordBytes, dataChunk...)
		checksum := calculateChecksum(recordBytes)

		dataHexString := ""
		for _, b := range dataChunk {
			dataHexString += fmt.Sprintf("%02X", b)
		}

		hexLines.WriteString(fmt.Sprintf(":%02X%04X%02X%s%02X\n", byteCount, addrField, recordType, dataHexString, checksum))
	}

	// --- Part 2: Process Configuration Words ---
	type sortedConfig struct {
		Name  string
		Value int
		Addr  int
	}
	var sortedConfigs []sortedConfig
	for name, value := range configWords {
		if configInfo, ok := g.mcConfig.ConfigWordDefaults[name]; ok {
			sortedConfigs = append(sortedConfigs, sortedConfig{name, value, configInfo.Address})
		}
	}
	sort.Slice(sortedConfigs, func(i, j int) bool {
		return sortedConfigs[i].Addr < sortedConfigs[j].Addr
	})

	currentELA := -1
	for _, config := range sortedConfigs {
		configInfo := g.mcConfig.ConfigWordDefaults[config.Name]
		configByteAddr := config.Addr * 2

		requiredELA := configByteAddr >> 16
		if requiredELA != currentELA {
			currentELA = requiredELA
			elaChecksum := calculateChecksum([]byte{0x02, 0x00, 0x00, 0x04, byte(currentELA >> 8), byte(currentELA)})
			hexLines.WriteString(fmt.Sprintf(":02000004%04X%02X\n", currentELA, elaChecksum))
		}

		mask := (1 << g.mcConfig.ProgramWordSizeBits) - 1
		paddedValue := (config.Value & mask) | configInfo.Padding
		dataBytes := []byte{byte(paddedValue & 0xFF), byte(paddedValue >> 8)}
		byteCount := 2
		recordAddrField := configByteAddr & 0xFFFF
		recordType := 0x00

		checksumInput := []byte{byte(byteCount), byte(recordAddrField >> 8), byte(recordAddrField), byte(recordType)}
		checksumInput = append(checksumInput, dataBytes...)
		checksum := calculateChecksum(checksumInput)
		dataHexString := fmt.Sprintf("%02X%02X", dataBytes[0], dataBytes[1])

		hexLines.WriteString(fmt.Sprintf(":%02X%04X%02X%s%02X\n", byteCount, recordAddrField, recordType, dataHexString, checksum))
	}

	// --- Part 3: End of File Record ---
	hexLines.WriteString(":00000001FF\n")

	return hexLines.String(), nil
}

// --- Main Assembly Function ---

// assemble is the main function to process assembly code.
func assemble(asmCodeString, hexFilePath string, mcConfig *MicrocontrollerConfig, reportFilePath string) error {
	// --- Step 1: Parse and expand macros ---
	parser := NewASMParser()
	parsedData, err := parser.Parse(asmCodeString)
	if err != nil {
		return fmt.Errorf("parsing failed: %w", err)
	}
	expandedData, err := parser.ExpandMacros(parsedData)
	if err != nil {
		return fmt.Errorf("macro expansion failed: %w", err)
	}

	// --- Step 2: Instantiate and run assembler ---
	assembler := NewPicAssembler(mcConfig, expandedData)
	if err := assembler.firstPass(); err != nil {
		return fmt.Errorf("first pass failed: %w", err)
	}
	if err := assembler.secondPass(); err != nil {
		return fmt.Errorf("second pass failed: %w", err)
	}

	// --- Step 3: Generate HEX file ---
	hexGenerator := NewHexGenerator(mcConfig)
	hexContent, err := hexGenerator.GenerateHex(assembler.machineCodeWords, assembler.configWords)
	if err != nil {
		return fmt.Errorf("HEX generation failed: %w", err)
	}

	if err := os.WriteFile(hexFilePath, []byte(hexContent), 0644); err != nil {
		return fmt.Errorf("failed to write HEX file: %w", err)
	}
	fmt.Printf("Assembly successful. HEX file generated at %s\n", hexFilePath)
	fmt.Printf("HEX file size: %d bytes\n", len(hexContent))

	// --- Step 4: Generate Report ---
	reportContent := assembler.GenerateReport(asmCodeString)
	if reportFilePath != "" {
		if err := os.WriteFile(reportFilePath, []byte(reportContent), 0644); err != nil {
			return fmt.Errorf("failed to write report file: %w", err)
		}
		fmt.Printf("Assembly report generated at %s\n", reportFilePath)
	} else {
		fmt.Println(reportContent)
	}

	return nil
}

// loadMicrocontrollerConfig reads and parses a JSON config file for a specific MCU.
func loadMicrocontrollerConfig(configPath string) (*MicrocontrollerConfig, error) {
	configFile, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("could not read config file '%s': %w", configPath, err)
	}

	var mcConfig MicrocontrollerConfig
	err = json.Unmarshal(configFile, &mcConfig)
	if err != nil {
		return nil, fmt.Errorf("could not parse JSON from '%s': %w", configPath, err)
	}

	return &mcConfig, nil
}

func main() {
	// Define command-line flags
	asmFile := flag.String("asm", "", "Path to the input assembly (.asm) file (required)")
	mcu := flag.String("mcu", "", "Target microcontroller name, e.g., 'PIC16F687' (required)")
	configDir := flag.String("config-dir", "./configs", "Directory containing microcontroller JSON config files")
	outFile := flag.String("hex", "", "Path to the output HEX file (defaults to <asm-file-name>.hex)")
	reportFile := flag.String("report", "", "Path to the output assembly report file (defaults to printing to console)")
	flag.Parse()

	// Validate required flags
	if *asmFile == "" || *mcu == "" {
		fmt.Println("Error: -asm and -mcu flags are required.")
		flag.Usage()
		os.Exit(1)
	}

	// --- Step 1: Load the MCU Configuration ---
	configPath := filepath.Join(*configDir, strings.ToLower(*mcu)+".json")
	mcConfig, err := loadMicrocontrollerConfig(configPath)
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}
	fmt.Printf("Configuration loaded for %s\n", *mcu)

	// --- Step 2: Read the Assembly Source Code ---
	asmCodeBytes, err := os.ReadFile(*asmFile)
	if err != nil {
		log.Fatalf("Error reading assembly file '%s': %v", *asmFile, err)
	}

	// --- Step 3: Determine Output Filenames ---
	hexFilePath := *outFile
	if hexFilePath == "" {
		baseName := strings.TrimSuffix(*asmFile, filepath.Ext(*asmFile))
		hexFilePath = baseName + ".hex"
	}

	// --- Step 4: Run the Assembler ---
	err = assemble(string(asmCodeBytes), hexFilePath, mcConfig, *reportFile)
	if err != nil {
		log.Fatalf("Assembly failed: %v", err)
	}
}
