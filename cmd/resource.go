// SPDX-License-Identifier: Apache-2.0
/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/CycloneDX/sbom-utility/common"
	"github.com/CycloneDX/sbom-utility/schema"
	"github.com/CycloneDX/sbom-utility/utils"
	"github.com/spf13/cobra"
)

const (
	SUBCOMMAND_RESOURCE_LIST = "list"
)

var VALID_SUBCOMMANDS_RESOURCE = []string{SUBCOMMAND_RESOURCE_LIST}

// filter keys
// Note: these string values MUST match annotations for the ResourceInfo struct fields
const (
	RESOURCE_FILTER_KEY_TYPE    = "type"
	RESOURCE_FILTER_KEY_NAME    = "name"
	RESOURCE_FILTER_KEY_VERSION = "version"
	RESOURCE_FILTER_KEY_BOMREF  = "bom-ref"
)

var VALID_RESOURCE_FILTER_KEYS = []string{
	RESOURCE_FILTER_KEY_TYPE,
	RESOURCE_FILTER_KEY_NAME,
	RESOURCE_FILTER_KEY_VERSION,
	RESOURCE_FILTER_KEY_BOMREF,
}

var RESOURCE_LIST_TITLES = []string{
	RESOURCE_FILTER_KEY_TYPE,
	RESOURCE_FILTER_KEY_NAME,
	RESOURCE_FILTER_KEY_VERSION,
	RESOURCE_FILTER_KEY_BOMREF,
}

// Flags. Reuse query flag values where possible
const (
	FLAG_RESOURCE_TYPE      = "type"
	FLAG_RESOURCE_TYPE_HELP = "filter output by resource type (i.e., component | service)"
)

const (
	MSG_OUTPUT_NO_RESOURCES_FOUND = "[WARN] no matching resources found for query"
)

// Command help formatting
const (
	FLAG_RESOURCE_OUTPUT_FORMAT_HELP = "format output using the specified type"
)

var RESOURCE_LIST_OUTPUT_SUPPORTED_FORMATS = MSG_SUPPORTED_OUTPUT_FORMATS_HELP +
	strings.Join([]string{FORMAT_TEXT, FORMAT_CSV, FORMAT_MARKDOWN}, ", ")

func NewCommandResource() *cobra.Command {
	var command = new(cobra.Command)
	command.Use = CMD_USAGE_RESOURCE_LIST
	command.Short = "Report on resources found in BOM input file"
	command.Long = "Report on resources found in BOM input file"
	command.Flags().StringVarP(&utils.GlobalFlags.PersistentFlags.OutputFormat, FLAG_FILE_OUTPUT_FORMAT, "", FORMAT_TEXT,
		FLAG_RESOURCE_OUTPUT_FORMAT_HELP+RESOURCE_LIST_OUTPUT_SUPPORTED_FORMATS)
	command.Flags().StringP(FLAG_RESOURCE_TYPE, "", schema.RESOURCE_TYPE_DEFAULT, FLAG_RESOURCE_TYPE_HELP)
	command.Flags().StringP(FLAG_REPORT_WHERE, "", "", FLAG_REPORT_WHERE_HELP)
	command.RunE = resourceCmdImpl
	command.ValidArgs = VALID_SUBCOMMANDS_RESOURCE
	command.PreRunE = func(cmd *cobra.Command, args []string) (err error) {
		// the command requires at least 1 valid subcommand (argument)
		if len(args) > 1 {
			return getLogger().Errorf("Too many arguments provided: %v", args)
		}

		// Make sure (optional) subcommand is known/valid
		if len(args) == 1 {
			if !preRunTestForSubcommand(command, VALID_SUBCOMMANDS_RESOURCE, args[0]) {
				return getLogger().Errorf("Subcommand provided is not valid: `%v`", args[0])
			}
		}

		if len(args) == 0 {
			getLogger().Tracef("No subcommands provided; defaulting to: `%s` subcommand", SUBCOMMAND_SCHEMA_LIST)
		}

		// Test for required flags (parameters)
		err = preRunTestForInputFile(cmd, args)

		return
	}
	return command
}

func retrieveResourceType(cmd *cobra.Command) (resourceType string, err error) {

	resourceType, err = cmd.Flags().GetString(FLAG_RESOURCE_TYPE)
	if err != nil {
		return
	}

	// validate resource type is a known keyword
	if !schema.IsValidResourceType(resourceType) {
		// invalid
		err = getLogger().Errorf("invalid resource `%s`: `%s`", FLAG_RESOURCE_TYPE, resourceType)
	}

	return
}

func resourceCmdImpl(cmd *cobra.Command, args []string) (err error) {
	getLogger().Enter(args)
	defer getLogger().Exit()

	// Create output writer
	outputFilename := utils.GlobalFlags.PersistentFlags.OutputFile
	outputFile, writer, err := createOutputFile(outputFilename)
	getLogger().Tracef("outputFile: `%v`; writer: `%v`", outputFilename, writer)

	// use function closure to assure consistent error output based upon error type
	defer func() {
		// always close the output file
		if outputFile != nil {
			outputFile.Close()
			getLogger().Infof("Closed output file: `%s`", outputFilename)
		}
	}()

	// process filters supplied on the --where command flag
	whereFilters, err := processWhereFlag(cmd)

	// Process flag: --type
	var resourceType string
	var resourceFlags utils.ResourceCommandFlags
	resourceType, err = retrieveResourceType(cmd)

	if err == nil {
		resourceFlags.ResourceType = resourceType
		err = ListResources(writer, utils.GlobalFlags.PersistentFlags, resourceFlags, whereFilters)
	}

	return
}

// Assure all errors are logged
func processResourceListResults(err error) {
	if err != nil {
		// No special processing at this time
		getLogger().Error(err)
	}
}

// NOTE: resourceType has already been validated
func ListResources(writer io.Writer, persistentFlags utils.PersistentCommandFlags, resourceFlags utils.ResourceCommandFlags, whereFilters []common.WhereFilter) (err error) {
	getLogger().Enter()
	defer getLogger().Exit()

	// use function closure to assure consistent error output based upon error type
	defer func() {
		if err != nil {
			processResourceListResults(err)
		}
	}()

	// Note: returns error if either file load or unmarshal to JSON map fails
	var document *schema.BOM
	document, err = LoadInputBOMFileAndDetectSchema()

	if err != nil {
		return
	}

	// Hash all licenses within input file
	getLogger().Infof("Scanning document for licenses...")
	err = loadDocumentResources(document, resourceFlags.ResourceType, whereFilters)

	if err != nil {
		return
	}

	format := persistentFlags.OutputFormat
	getLogger().Infof("Outputting listing (`%s` format)...", format)
	switch format {
	case FORMAT_TEXT:
		DisplayResourceListText(document, writer)
	case FORMAT_CSV:
		DisplayResourceListCSV(document, writer)
	case FORMAT_MARKDOWN:
		DisplayResourceListMarkdown(document, writer)
	default:
		// Default to Text output for anything else (set as flag default)
		getLogger().Warningf("Listing not supported for `%s` format; defaulting to `%s` format...",
			format, FORMAT_TEXT)
		DisplayResourceListText(document, writer)
	}

	return
}

func loadDocumentResources(document *schema.BOM, resourceType string, whereFilters []common.WhereFilter) (err error) {
	getLogger().Enter()
	defer getLogger().Exit(err)

	// At this time, fail SPDX format SBOMs as "unsupported" (for "any" format)
	if !document.FormatInfo.IsCycloneDx() {
		err = schema.NewUnsupportedFormatForCommandError(
			document.FormatInfo.CanonicalName,
			document.GetFilename(),
			CMD_LICENSE, FORMAT_ANY)
		return
	}

	// Before looking for license data, fully unmarshal the SBOM into named structures
	if err = document.UnmarshalCycloneDXBOM(); err != nil {
		return
	}

	// Add top-level SBOM component
	if resourceType == schema.RESOURCE_TYPE_DEFAULT || resourceType == schema.RESOURCE_TYPE_COMPONENT {
		err = document.HashComponentResources(whereFilters)
		if err != nil {
			return
		}
	}

	if resourceType == schema.RESOURCE_TYPE_DEFAULT || resourceType == schema.RESOURCE_TYPE_SERVICE {
		err = document.HashServiceResources(whereFilters)
		if err != nil {
			return
		}
	}

	return
}

// NOTE: This list is NOT de-duplicated
// TODO: Add a --no-title flag to skip title output
func DisplayResourceListText(bom *schema.BOM, writer io.Writer) {
	getLogger().Enter()
	defer getLogger().Exit()

	// initialize tabwriter
	w := new(tabwriter.Writer)
	defer w.Flush()

	// min-width, tab-width, padding, pad-char, flags
	w.Init(writer, 8, 2, 2, ' ', 0)

	// create underline row from compulsory titles
	underlines := createTitleTextSeparators(RESOURCE_LIST_TITLES)

	// Add tabs between column titles for the tabWRiter
	fmt.Fprintf(w, "%s\n", strings.Join(RESOURCE_LIST_TITLES, "\t"))
	fmt.Fprintf(w, "%s\n", strings.Join(underlines, "\t"))

	// Display a warning "missing" in the actual output and return (short-circuit)
	entries := bom.ResourceMap.Entries()

	// Emit no license warning into output
	if len(entries) == 0 {
		fmt.Fprintf(w, "%s\n", MSG_OUTPUT_NO_RESOURCES_FOUND)
		return
	}

	// Sort by Type then Name
	sort.Slice(entries, func(i, j int) bool {
		resource1 := (entries[i].Value).(schema.CDXResourceInfo)
		resource2 := (entries[j].Value).(schema.CDXResourceInfo)
		if resource1.Type != resource2.Type {
			return resource1.Type < resource2.Type
		}

		return resource1.Name < resource2.Name
	})

	var resourceInfo schema.CDXResourceInfo

	for _, entry := range entries {
		value := entry.Value
		resourceInfo = value.(schema.CDXResourceInfo)

		// Format line and write to output
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			resourceInfo.Type,
			resourceInfo.Name,
			resourceInfo.Version,
			resourceInfo.BOMRef)
	}
}

// TODO: Add a --no-title flag to skip title output
func DisplayResourceListCSV(bom *schema.BOM, writer io.Writer) (err error) {
	getLogger().Enter()
	defer getLogger().Exit()

	// initialize writer and prepare the list of entries (i.e., the "rows")
	w := csv.NewWriter(writer)
	defer w.Flush()

	if err = w.Write(RESOURCE_LIST_TITLES); err != nil {
		return getLogger().Errorf("error writing to output (%v): %s", RESOURCE_LIST_TITLES, err)
	}

	// Display a warning "missing" in the actual output and return (short-circuit)
	entries := bom.ResourceMap.Entries()

	// Emit no resource found warning into output
	if len(entries) == 0 {
		currentRow := []string{MSG_OUTPUT_NO_RESOURCES_FOUND}
		if err = w.Write(currentRow); err != nil {
			// unable to emit an error message into output stream
			return getLogger().Errorf("error writing to output (%v): %s", currentRow, err)
		}
		return fmt.Errorf(currentRow[0])
	}

	// Sort by Type
	sort.Slice(entries, func(i, j int) bool {
		resource1 := (entries[i].Value).(schema.CDXResourceInfo)
		resource2 := (entries[j].Value).(schema.CDXResourceInfo)
		if resource1.Type != resource2.Type {
			return resource1.Type < resource2.Type
		}

		return resource1.Name < resource2.Name
	})

	var resourceInfo schema.CDXResourceInfo
	var line []string

	for _, entry := range entries {
		value := entry.Value
		resourceInfo = value.(schema.CDXResourceInfo)
		line = nil
		line = append(line,
			resourceInfo.Type,
			resourceInfo.Name,
			resourceInfo.Version,
			resourceInfo.BOMRef,
		)

		if err = w.Write(line); err != nil {
			err = getLogger().Errorf("csv.Write: %w", err)
		}
	}

	return
}

// TODO: Add a --no-title flag to skip title output
func DisplayResourceListMarkdown(bom *schema.BOM, writer io.Writer) (err error) {
	getLogger().Enter()
	defer getLogger().Exit()

	// create title row
	titleRow := createMarkdownRow(RESOURCE_LIST_TITLES)
	fmt.Fprintf(writer, "%s\n", titleRow)

	alignments := createMarkdownColumnAlignment(RESOURCE_LIST_TITLES)
	alignmentRow := createMarkdownRow(alignments)
	fmt.Fprintf(writer, "%s\n", alignmentRow)

	// Display a warning "missing" in the actual output and return (short-circuit)
	entries := bom.ResourceMap.Entries()

	// Emit no resource found warning into output
	if len(entries) == 0 {
		fmt.Fprintf(writer, "%s\n", MSG_OUTPUT_NO_RESOURCES_FOUND)
		return fmt.Errorf(MSG_OUTPUT_NO_RESOURCES_FOUND)
	}

	// Sort by Type
	sort.Slice(entries, func(i, j int) bool {
		resource1 := (entries[i].Value).(schema.CDXResourceInfo)
		resource2 := (entries[j].Value).(schema.CDXResourceInfo)
		if resource1.Type != resource2.Type {
			return resource1.Type < resource2.Type
		}

		return resource1.Name < resource2.Name
	})

	var resourceInfo schema.CDXResourceInfo
	var line []string
	var lineRow string

	for _, entry := range entries {
		value := entry.Value
		resourceInfo = value.(schema.CDXResourceInfo)
		// reset current line
		line = nil

		line = append(line,
			resourceInfo.Type,
			resourceInfo.Name,
			resourceInfo.Version,
			resourceInfo.BOMRef,
		)

		lineRow = createMarkdownRow(line)
		fmt.Fprintf(writer, "%s\n", lineRow)
	}

	return
}
