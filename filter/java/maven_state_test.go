package java

import "testing"

func TestClassifyLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected MavenLineClass
	}{
		// LineDiscovery
		{"discovery", "[INFO] Scanning for projects...", LineDiscovery},

		// LineModuleHeader
		{"module header", "[INFO] Building myapp 1.0.0 [3/20]", LineModuleHeader},
		{"module header simple", "[INFO] Building core-key-business 2.0.0", LineModuleHeader},

		// LineMojoHeader
		{"mojo header kotlin", "[INFO] --- kotlin-maven-plugin:2.2.0:compile (compile) @ core-key-business ---", LineMojoHeader},
		{"mojo header resources", "[INFO] --- maven-resources-plugin:3.3.0:resources (default-resources) @ myapp ---", LineMojoHeader},
		{"mojo header surefire", "[INFO] --- maven-surefire-plugin:3.0.0:test (default-test) @ myapp ---", LineMojoHeader},

		// LineTransfer
		{"downloading", "[INFO] Downloading from central: https://repo.maven.apache.org/maven2/org/foo/1.0/foo-1.0.pom", LineTransfer},
		{"downloaded", "[INFO] Downloaded from central: https://repo.maven.apache.org/maven2/org/foo/1.0/foo-1.0.jar (10 kB at 100 kB/s)", LineTransfer},

		// LinePomWarning
		{"pom warning", "[WARNING] Some problems were encountered while building the effective model for com.example:app:jar:1.0", LinePomWarning},
		{"pom warning systemPath", "[WARNING] 'dependencies.dependency.systemPath' for com.example:lib:jar should not point at files within the project directory", LinePomWarning},

		// LineCompilerWarning
		{"compiler warning kotlin", "[WARNING] file:///app/src/Foo.kt:42:5 The corresponding parameter in the supertype", LineCompilerWarning},
		{"compiler warning open", "[WARNING] file:///app/src/Bar.kt:10:1 Modifier 'open' has no effect on a final class", LineCompilerWarning},

		// LineReactorHeader
		{"reactor header", "[INFO] Reactor Summary for parent-project 1.0.0:", LineReactorHeader},
		{"reactor header simple", "[INFO] Reactor Summary:", LineReactorHeader},

		// LineReactorEntry
		{"reactor success", "[INFO] core-key-business ......................... SUCCESS [  3.456 s]", LineReactorEntry},
		{"reactor failure", "[INFO] core-key-api ............................. FAILURE [  1.234 s]", LineReactorEntry},
		{"reactor skipped", "[INFO] core-key-web ............................. SKIPPED", LineReactorEntry},

		// LineBuildResult
		{"build success", "[INFO] BUILD SUCCESS", LineBuildResult},
		{"build failure", "[INFO] BUILD FAILURE", LineBuildResult},

		// LineStats
		{"total time", "[INFO] Total time:  01:23 min", LineStats},

		// LineFinishedAt
		{"finished at", "[INFO] Finished at: 2024-01-15T10:30:00+08:00", LineFinishedAt},

		// LineSeparator
		{"separator", "[INFO] ------------------------------------------------------------------------", LineSeparator},

		// LineError
		{"error line", "[ERROR] Failed to execute goal org.apache.maven.plugins:maven-compiler-plugin:3.11.0:compile", LineError},
		{"error unresolved", "[ERROR] /app/src/Foo.kt:[42,5] Unresolved reference: bar", LineError},

		// LineTestHeader
		{"test header", "[INFO] T E S T S", LineTestHeader},

		// LineTestSummary (INFO)
		{"test summary info", "[INFO] Tests run: 10, Failures: 0, Errors: 0, Skipped: 0", LineTestSummary},
		// LineTestSummary (ERROR)
		{"test summary error", "[ERROR] Tests run: 10, Failures: 2, Errors: 0, Skipped: 0", LineTestSummary},

		// LineTestRunning
		{"test running", "[INFO] Running com.example.MyTest", LineTestRunning},

		// LineStackTrace
		{"stack at", "	at org.apache.maven.lifecycle.internal.MojoExecutor.execute(MojoExecutor.java:123)", LineStackTrace},
		{"stack org", "org.apache.maven.plugin.MojoFailureException: Compilation failure", LineStackTrace},
		{"stack java", "java.lang.RuntimeException: Something went wrong", LineStackTrace},

		// LineHelpSuggestion
		{"help stack trace", "[ERROR] To see the full stack trace of the errors, re-run Maven with the -e switch.", LineHelpSuggestion},
		{"help rerun", "[ERROR] Re-run Maven using the -X switch to enable full debug logging.", LineHelpSuggestion},
		{"help 1", "[ERROR] [Help 1] http://cwiki.apache.org/confluence/display/MAVEN/MojoFailureException", LineHelpSuggestion},
		{"help after correcting", "[ERROR] After correcting the problems, you can resume the build with the command", LineHelpSuggestion},

		// LineEmpty
		{"empty string", "", LineEmpty},
		{"info empty", "[INFO]", LineEmpty},
		{"error empty", "[ERROR]", LineEmpty},
		{"warning empty", "[WARNING]", LineEmpty},

		// LineProcessNoise
		{"compiling", "[INFO] Compiling 42 source files to /app/target/classes", LineProcessNoise},
		{"nothing to compile", "[INFO] Nothing to compile - all classes are up to date", LineProcessNoise},
		{"copying", "[INFO] Copying 3 resources", LineProcessNoise},
		{"using encoding", "[INFO] Using 'UTF-8' encoding to copy filtered resources.", LineProcessNoise},
		{"changes detected", "[INFO] Changes detected - recompiling the module!", LineProcessNoise},
		{"skip non existing", "[INFO] skip non existing resourceDirectory /app/src/main/resources", LineProcessNoise},
		{"using auto detected", "[INFO] Using auto detected provider org.apache.maven.surefire.junit4.JUnit4Provider", LineProcessNoise},

		// LinePluginOutput
		{"plugin output", "[INFO] Some other plugin output line", LinePluginOutput},
		{"plain text", "some plain text without prefix", LinePluginOutput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyLine(tt.line)
			if got != tt.expected {
				t.Errorf("classifyLine(%q) = %d, want %d", tt.line, got, tt.expected)
			}
		})
	}
}

func TestStateTransition_ConsecutiveMojoHeaders(t *testing.T) {
	// Regression: consecutive --- plugin --- lines should stay in StateMojo
	state := StateInit
	state = nextState(state, LineModuleHeader) // -> StateModuleBuild
	state = nextState(state, LineMojoHeader)   // -> StateMojo (global)

	// First non-mojo line -> PluginOutput
	state = nextState(state, LinePluginOutput)
	if state != StatePluginOutput {
		t.Errorf("after plugin output: got %v, want StatePluginOutput", state)
	}

	// Second MojoHeader -> back to StateMojo (not PluginOutput!)
	state = nextState(state, LineMojoHeader)
	if state != StateMojo {
		t.Errorf("second MojoHeader from PluginOutput: got %v, want StateMojo", state)
	}

	// Third MojoHeader directly -> stay StateMojo
	state = nextState(state, LineMojoHeader)
	if state != StateMojo {
		t.Errorf("third consecutive MojoHeader: got %v, want StateMojo", state)
	}
}

func TestStateTransition(t *testing.T) {
	tests := []struct {
		name     string
		from     MavenState
		input    MavenLineClass
		expected MavenState
	}{
		// Init 阶段
		{"init to discovery", StateInit, LineDiscovery, StateDiscovery},
		{"init stays on noise", StateInit, LineProcessNoise, StateInit},

		// Discovery 阶段
		{"discovery to warning", StateDiscovery, LinePomWarning, StateWarning},
		{"discovery stays on transfer", StateDiscovery, LineTransfer, StateDiscovery},

		// Warning 阶段
		{"warning stays on warning", StateWarning, LinePomWarning, StateWarning},
		{"warning stays on empty", StateWarning, LineEmpty, StateWarning},
		{"warning stays on other", StateWarning, LinePluginOutput, StateWarning},

		// 全局转移: 任意状态 → ModuleBuild
		{"global module header from discovery", StateDiscovery, LineModuleHeader, StateModuleBuild},
		{"global module header from warning", StateWarning, LineModuleHeader, StateModuleBuild},

		// ModuleBuild 阶段
		{"modulebuild to mojo", StateModuleBuild, LineMojoHeader, StateMojo},
		{"modulebuild stays on separator", StateModuleBuild, LineSeparator, StateModuleBuild},

		// Mojo → PluginOutput（任意非全局行）
		{"mojo to plugin output", StateMojo, LinePluginOutput, StatePluginOutput},
		{"mojo to plugin output on noise", StateMojo, LineProcessNoise, StatePluginOutput},

		// PluginOutput 阶段
		{"plugin output to mojo", StatePluginOutput, LineMojoHeader, StateMojo},
		{"plugin output to test", StatePluginOutput, LineTestHeader, StateTestOutput},
		{"plugin output stays on other", StatePluginOutput, LinePluginOutput, StatePluginOutput},

		// TestOutput 阶段
		{"test output to mojo", StateTestOutput, LineMojoHeader, StateMojo},
		{"test output stays on running", StateTestOutput, LineTestRunning, StateTestOutput},

		// 全局转移: Reactor
		{"global reactor from test", StateTestOutput, LineReactorHeader, StateReactor},

		// 全局转移: BuildResult
		{"global build result from reactor", StateReactor, LineBuildResult, StateResult},

		// Result 阶段
		{"result to error report", StateResult, LineError, StateErrorReport},
		{"result stays on separator", StateResult, LineSeparator, StateResult},

		// 全局转移: Stats
		{"global stats from result", StateResult, LineStats, StateStats},

		// ErrorReport 阶段
		{"error report stays on stack", StateErrorReport, LineStackTrace, StateErrorReport},
		{"error report stays on help", StateErrorReport, LineHelpSuggestion, StateErrorReport},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextState(tt.from, tt.input)
			if got != tt.expected {
				t.Errorf("nextState(%d, %d) = %d, want %d", tt.from, tt.input, got, tt.expected)
			}
		})
	}
}
