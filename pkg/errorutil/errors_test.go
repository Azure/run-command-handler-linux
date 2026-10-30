package errorutil

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/stretchr/testify/require"
)

func TestWithCode(t *testing.T) {
	cause := errors.New("cause")
	require.NoError(t, WithCode(1, nil))
	for _, original := range []error{
		vmextension.NewErrorWithClarificationPtr(25, cause),
		vmextension.NewErrorWithClarification(-25, cause),
		fmt.Errorf("context: %w", error(vmextension.NewErrorWithClarificationPtr(26, cause))),
		fmt.Errorf("context: %w", vmextension.NewErrorWithClarification(-26, cause)),
	} {
		expected, _ := Code(original)
		err := WithCode(999, original)
		require.Equal(t, original, err)
		code, ok := Code(err)
		require.True(t, ok)
		require.Equal(t, expected, code)
		require.ErrorIs(t, err, cause)
	}
	for _, code := range []int{1, -1} {
		err := WithCode(code, cause)
		actual, ok := Code(err)
		require.True(t, ok)
		require.Equal(t, code, actual)
		require.ErrorIs(t, err, cause)
	}
	_, ok := Code(cause)
	require.False(t, ok)
	_, ok = Code(nil)
	require.False(t, ok)
	joined := errors.Join(vmextension.NewErrorWithClarification(27, cause), vmextension.NewErrorWithClarificationPtr(28, cause))
	code, ok := Code(joined)
	require.True(t, ok)
	require.Equal(t, 27, code, "pointer errors must not take precedence over earlier value errors")
	code, ok = Code(errors.Join(cause, vmextension.NewErrorWithClarificationPtr(29, cause)))
	require.True(t, ok)
	require.Equal(t, 29, code)
	_, ok = Code(errors.Join(cause, errors.New("another unclassified cause")))
	require.False(t, ok)
	var nilClarification *vmextension.ErrorWithClarification
	_, ok = Code(nilClarification)
	require.False(t, ok)
}

func TestCustomScriptClarificationCodeCompatibility(t *testing.T) {
	for name, tc := range map[string]struct {
		code ErrorClarificationCode
		want int
	}{
		"Storage_internalServerError":                         {Storage_internalServerError, -1},
		"Internal_badConfig":                                  {Internal_badConfig, -21},
		"Imds_internalMsiError":                               {Imds_internalMsiError, -30},
		"FileDownload_badRequest":                             {FileDownload_badRequest, -41},
		"Os_FailedToDeleteDataDir":                            {Os_FailedToDeleteDataDir, -50},
		"Os_FailedToOpenStdOut":                               {Os_FailedToOpenStdOut, -51},
		"Os_FailedToOpenStdErr":                               {Os_FailedToOpenStdErr, -52},
		"CommandExecution_failedUnknownError":                 {CommandExecution_failedUnknownError, 1},
		"CommandExecution_failureExitCode":                    {CommandExecution_failureExitCode, 2},
		"CommandExecution_interruptedByVmShutdown":            {CommandExecution_interruptedByVmShutdown, 3},
		"CustomerInput_commandToExecuteAndScriptNotSpecified": {CustomerInput_commandToExecuteAndScriptNotSpecified, 23},
		"CustomerInput_clientIdObjectIdBothSpecified":         {CustomerInput_clientIdObjectIdBothSpecified, 27},
		"FileDownload_unableToCreateDownloadDirectory":        {FileDownload_unableToCreateDownloadDirectory, 50},
		"FileDownload_accessDenied":                           {FileDownload_accessDenied, 52},
		"FileDownload_doesNotExist":                           {FileDownload_doesNotExist, 53},
		"FileDownload_networkingError":                        {FileDownload_networkingError, 54},
		"FileDownload_genericError":                           {FileDownload_genericError, 55},
		"FileDownload_exceededTimeout":                        {FileDownload_exceededTimeout, 56},
		"Msi_notFound":                                        {Msi_notFound, 70},
		"Msi_doesNotHaveRightPermissions":                     {Msi_doesNotHaveRightPermissions, 71},
		"Msi_GenericRetrievalError":                           {Msi_GenericRetrievalError, 72},
		"ExtensionPolicySettings_invalidPolicyFileFormat":     {ExtensionPolicySettings_invalidPolicyFileFormat, 80},
		"ExtensionPolicySettings_policyLoadFailed":            {ExtensionPolicySettings_policyLoadFailed, 81},
		"ExtensionPolicySettings_downloadedScriptNotAllowed":  {ExtensionPolicySettings_downloadedScriptNotAllowed, 82},
		"ExtensionPolicySettings_commandToExecuteNotAllowed":  {ExtensionPolicySettings_commandToExecuteNotAllowed, 83},
		"ExtensionPolicySettings_scriptNotAllowed":            {ExtensionPolicySettings_scriptNotAllowed, 84},
	} {
		t.Run(name, func(t *testing.T) { require.Equal(t, tc.want, tc.code) })
	}
}

// Each named code must identify exactly one production classification site.
func TestClarificationCodesHaveUniqueSites(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	files := token.NewFileSet()
	registry, err := parser.ParseFile(files, "errorclarificationcodes.go", nil, 0)
	require.NoError(t, err)
	codes := map[string]int{}
	values := map[int]string{}
	for _, declaration := range registry.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		for _, spec := range group.Specs {
			value := spec.(*ast.ValueSpec)
			require.Len(t, value.Names, 1)
			require.Len(t, value.Values, 1)
			codeType, ok := value.Type.(*ast.Ident)
			require.True(t, ok)
			require.Equal(t, "ErrorClarificationCode", codeType.Name)
			expression := value.Values[0]
			sign := 1
			if unary, ok := expression.(*ast.UnaryExpr); ok && unary.Op == token.SUB {
				sign, expression = -1, unary.X
			}
			literal, ok := expression.(*ast.BasicLit)
			require.True(t, ok, "code values must be explicit integers")
			code, err := strconv.Atoi(literal.Value)
			require.NoError(t, err)
			code *= sign
			require.NotZero(t, code)
			name := value.Names[0].Name
			if previous, exists := values[code]; exists {
				t.Errorf("%s and %s both declare code %d", previous, name, code)
			}
			codes[name], values[code] = code, name
		}
	}
	require.NotEmpty(t, codes)

	sites := map[string]string{}
	tested := map[string]bool{}
	identifierPattern := regexp.MustCompile(`[A-Za-z_][A-Za-z_0-9]*`)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(files, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, "_test.go") {
			ast.Inspect(file, func(node ast.Node) bool {
				if identifier, ok := node.(*ast.Ident); ok {
					if _, exists := codes[identifier.Name]; exists {
						tested[identifier.Name] = true
					}
				}
				return true
			})
			for _, comment := range file.Comments {
				for _, name := range identifierPattern.FindAllString(comment.Text(), -1) {
					if _, exists := codes[name]; exists {
						tested[name] = true
					}
				}
			}
			return nil
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "WithCode" {
				return true
			}
			argument, ok := call.Args[0].(*ast.SelectorExpr)
			location := files.Position(call.Pos()).String()
			if !ok {
				t.Errorf("use a central ErrorClarificationCode constant at %s", location)
				return true
			}
			name := argument.Sel.Name
			if _, exists := codes[name]; !exists {
				t.Errorf("unregistered clarification constant %s at %s", name, location)
			}
			if previous, exists := sites[name]; exists {
				t.Errorf("%s reused at %s and %s", name, previous, location)
			}
			sites[name] = location
			return true
		})
		return nil
	})
	require.NoError(t, err)
	for name := range codes {
		if sites[name] == "" {
			t.Errorf("%s needs exactly one production classification site", name)
		}
		if !tested[name] {
			t.Errorf("%s needs an error-path test or a documented untestable-path reason", name)
		}
	}
	t.Logf("verified %d unique named clarification sites", len(sites))
}
