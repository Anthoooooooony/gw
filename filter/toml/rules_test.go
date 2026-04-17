package toml

import (
	"strings"
	"testing"

	"github.com/gw-cli/gw/filter"
)

// 通过 LoadBuiltinRules 的引擎实例运行真实命令样本。
// 每条新规则至少包含两类用例：
//   - noise-drop：噪音行被 strip
//   - key-preserve：错误/摘要等关键行被保留

func runBuiltin(t *testing.T, cmd string, args []string, stdout string) string {
	t.Helper()
	f, err := LoadBuiltinRules()
	if err != nil {
		t.Fatalf("加载内置规则失败: %v", err)
	}
	if !f.Match(cmd, args) {
		t.Fatalf("内置规则未匹配 %s %v", cmd, args)
	}
	out := f.Apply(filter.FilterInput{Cmd: cmd, Args: args, Stdout: stdout})
	return out.Content
}

// ---------------- npm ----------------

func TestRule_NpmInstall_DropsNoise(t *testing.T) {
	// 真实 `npm install` 输出片段（含进度、deprecated 警告、funding 提示）
	input := strings.Join([]string{
		"npm WARN deprecated rimraf@2.6.3: Rimraf versions prior to v4 are no longer supported",
		"npm WARN deprecated har-validator@5.1.5: this library is no longer supported",
		"npm WARN ERESOLVE overriding peer dependency",
		"added 245 packages, and audited 246 packages in 12s",
		"38 packages are looking for funding",
		"  run `npm fund` for details",
		"found 0 vulnerabilities",
	}, "\n")

	got := runBuiltin(t, "npm", []string{"install"}, input)

	for _, noise := range []string{
		"npm WARN deprecated",
		"npm WARN ERESOLVE",
		"added 245 packages",
		"are looking for funding",
		"npm fund",
	} {
		if strings.Contains(got, noise) {
			t.Errorf("噪音应被移除但仍存在: %q\n输出:\n%s", noise, got)
		}
	}
}

func TestRule_NpmInstall_PreservesErrors(t *testing.T) {
	input := strings.Join([]string{
		"npm WARN deprecated foo@1.0.0",
		"npm ERR! code ENOENT",
		"npm ERR! syscall open",
		"npm ERR! path /tmp/missing/package.json",
		"found 0 vulnerabilities",
	}, "\n")

	got := runBuiltin(t, "npm", []string{"install"}, input)

	for _, key := range []string{"npm ERR! code ENOENT", "npm ERR! syscall", "npm ERR! path"} {
		if !strings.Contains(got, key) {
			t.Errorf("关键错误行应保留: %q\n输出:\n%s", key, got)
		}
	}
}

func TestRule_NpmTest_PreservesFailureSummary(t *testing.T) {
	// jest 风格输出
	input := strings.Join([]string{
		"> myapp@1.0.0 test",
		"> jest",
		"PASS src/util.test.js",
		"  ✓ should add (3ms)",
		"FAIL src/api.test.js",
		"  ● API › should fetch user",
		"    expect(received).toEqual(expected)",
		"Tests:       1 failed, 5 passed, 6 total",
	}, "\n")

	got := runBuiltin(t, "npm", []string{"test"}, input)

	if strings.Contains(got, "PASS src/util.test.js") {
		t.Errorf("PASS 行应被 strip，输出:\n%s", got)
	}
	for _, key := range []string{"FAIL src/api.test.js", "1 failed, 5 passed", "expect(received)"} {
		if !strings.Contains(got, key) {
			t.Errorf("关键失败信息应保留: %q\n输出:\n%s", key, got)
		}
	}
}

func TestRule_NpmRunBuild_PreservesErrors(t *testing.T) {
	// vite/webpack 风格的 build 输出
	input := strings.Join([]string{
		"> myapp@1.0.0 build",
		"> vite build",
		"vite v4.4.9 building for production...",
		"  dist/assets/index-abc.js  142.3 kB │ gzip: 45.2 kB",
		"  dist/assets/style.css      8.7 kB │ gzip:  2.1 kB",
		"✓ built in 3.45s",
	}, "\n")

	got := runBuiltin(t, "npm", []string{"run", "build"}, input)

	if strings.Contains(got, "dist/assets/index-abc.js") {
		t.Errorf("逐文件 bundle 行应被 strip，输出:\n%s", got)
	}
	if !strings.Contains(got, "built in 3.45s") {
		t.Errorf("最终摘要应保留，输出:\n%s", got)
	}
}

// ---------------- yarn ----------------

func TestRule_YarnInstall_DropsProgress(t *testing.T) {
	input := strings.Join([]string{
		"yarn install v1.22.19",
		"[1/4] Resolving packages...",
		"[2/4] Fetching packages...",
		"[3/4] Linking dependencies...",
		"[4/4] Building fresh packages...",
		"warning react > prop-types@15.8.1 has unmet peer dependency \"react@>=15\"",
		"info All dependencies",
		"Done in 8.23s.",
	}, "\n")

	got := runBuiltin(t, "yarn", []string{"install"}, input)

	for _, noise := range []string{"[1/4]", "[2/4]", "[3/4]", "[4/4]", "info All dependencies", "has unmet peer"} {
		if strings.Contains(got, noise) {
			t.Errorf("噪音应被移除: %q\n输出:\n%s", noise, got)
		}
	}
	if !strings.Contains(got, "Done in 8.23s") {
		t.Errorf("Done 行应保留，输出:\n%s", got)
	}
}

// ---------------- pnpm ----------------

func TestRule_PnpmInstall_DropsProgress(t *testing.T) {
	input := strings.Join([]string{
		"Progress: resolved 412, reused 410, downloaded 2, added 0",
		"Packages: +245",
		"++++++++++++++++++++++++++++++++++++++++++++++++++++++",
		"+ react 18.2.0",
		"+ typescript 5.3.3",
		" WARN  deprecated har-validator@5.1.5",
		"Already up to date",
		"Done in 2.1s",
	}, "\n")

	got := runBuiltin(t, "pnpm", []string{"install"}, input)

	for _, noise := range []string{"Progress: resolved", "Packages: +", "+ react 18.2.0", "Already up to date", " WARN  deprecated"} {
		if strings.Contains(got, noise) {
			t.Errorf("噪音应被移除: %q\n输出:\n%s", noise, got)
		}
	}
	if !strings.Contains(got, "Done in 2.1s") {
		t.Errorf("Done 行应保留，输出:\n%s", got)
	}
}

func TestRule_PnpmInstall_PreservesErrors(t *testing.T) {
	input := strings.Join([]string{
		"Progress: resolved 100, reused 95, downloaded 5",
		" ERR_PNPM_FETCH_404 GET https://registry.npmjs.org/missing-pkg: Not Found - 404",
		"This error happened while installing a direct dependency of /workspace",
	}, "\n")

	got := runBuiltin(t, "pnpm", []string{"install"}, input)

	for _, key := range []string{"ERR_PNPM_FETCH_404", "This error happened while installing"} {
		if !strings.Contains(got, key) {
			t.Errorf("错误行应保留: %q\n输出:\n%s", key, got)
		}
	}
}

// ---------------- pip ----------------

func TestRule_PipInstall_DropsCacheNoise(t *testing.T) {
	input := strings.Join([]string{
		"Collecting requests",
		"  Downloading requests-2.31.0-py3-none-any.whl (62 kB)",
		"  Using cached charset_normalizer-3.3.2-py3-none-any.whl (118 kB)",
		"Requirement already satisfied: idna in /venv/lib (3.6)",
		"Requirement already satisfied: certifi in /venv/lib (2024.2.2)",
		"Installing collected packages: requests",
		"Successfully installed requests-2.31.0",
		"[notice] A new release of pip is available: 23.2.1 -> 24.0",
		"[notice] To update, run: pip install --upgrade pip",
	}, "\n")

	got := runBuiltin(t, "pip", []string{"install", "requests"}, input)

	for _, noise := range []string{
		"Requirement already satisfied",
		"Downloading requests-",
		"Using cached charset_normalizer",
		"Collecting requests",
		"[notice] A new release",
		"[notice] To update",
	} {
		if strings.Contains(got, noise) {
			t.Errorf("噪音应被移除: %q\n输出:\n%s", noise, got)
		}
	}
	if !strings.Contains(got, "Successfully installed requests-2.31.0") {
		t.Errorf("Successfully installed 摘要应保留，输出:\n%s", got)
	}
}

func TestRule_PipInstall_PreservesErrors(t *testing.T) {
	input := strings.Join([]string{
		"Collecting nonexistent-pkg",
		"ERROR: Could not find a version that satisfies the requirement nonexistent-pkg",
		"ERROR: No matching distribution found for nonexistent-pkg",
	}, "\n")

	got := runBuiltin(t, "pip", []string{"install", "nonexistent-pkg"}, input)

	for _, key := range []string{"Could not find a version", "No matching distribution"} {
		if !strings.Contains(got, key) {
			t.Errorf("错误行应保留: %q\n输出:\n%s", key, got)
		}
	}
}

// ---------------- pytest ----------------

func TestRule_Pytest_DropsHeaderAndProgress(t *testing.T) {
	input := strings.Join([]string{
		"============================= test session starts ==============================",
		"platform linux -- Python 3.11.7, pytest-8.0.0, pluggy-1.4.0",
		"cachedir: .pytest_cache",
		"rootdir: /workspace/myproj",
		"configfile: pyproject.toml",
		"plugins: cov-4.1.0, asyncio-0.23.4",
		"collecting ...",
		"collected 42 items",
		"",
		"tests/test_api.py ........F.s.                                            [ 30%]",
		"tests/test_models.py ..................                                   [ 80%]",
		"tests/test_views.py ........                                              [100%]",
		"",
		"=================================== FAILURES ===================================",
		"_____________________________ test_user_create _________________________________",
		"    def test_user_create():",
		">       assert User.objects.count() == 1",
		"E       AssertionError: assert 0 == 1",
		"=========================== short test summary info ============================",
		"FAILED tests/test_api.py::test_user_create - AssertionError",
		"========================= 1 failed, 40 passed, 1 skipped in 2.34s ==============",
	}, "\n")

	got := runBuiltin(t, "pytest", []string{}, input)

	for _, noise := range []string{
		"platform linux",
		"cachedir: ",
		"rootdir: ",
		"configfile: ",
		"plugins: ",
		"tests/test_api.py ........F.s.",
		"tests/test_models.py ..................",
	} {
		if strings.Contains(got, noise) {
			t.Errorf("噪音应被移除: %q\n输出:\n%s", noise, got)
		}
	}
	for _, key := range []string{
		"FAILURES",
		"FAILED tests/test_api.py::test_user_create",
		"AssertionError",
		"1 failed, 40 passed",
	} {
		if !strings.Contains(got, key) {
			t.Errorf("关键信息应保留: %q\n输出:\n%s", key, got)
		}
	}
}

// ---------------- cargo ----------------

func TestRule_CargoBuild_DropsCompileProgress(t *testing.T) {
	input := strings.Join([]string{
		"    Updating crates.io index",
		"  Downloaded serde v1.0.196",
		"  Downloaded tokio v1.36.0",
		"   Compiling proc-macro2 v1.0.78",
		"   Compiling unicode-ident v1.0.12",
		"   Compiling syn v2.0.50",
		"   Compiling serde v1.0.196",
		"   Compiling mycrate v0.1.0 (/workspace/mycrate)",
		"warning: unused variable: `x`",
		"  --> src/main.rs:5:9",
		"    Finished dev [unoptimized + debuginfo] target(s) in 12.34s",
	}, "\n")

	got := runBuiltin(t, "cargo", []string{"build"}, input)

	for _, noise := range []string{
		"Updating crates.io index",
		"Downloaded serde",
		"Compiling proc-macro2",
		"Compiling syn",
	} {
		if strings.Contains(got, noise) {
			t.Errorf("噪音应被移除: %q\n输出:\n%s", noise, got)
		}
	}
	for _, key := range []string{
		"warning: unused variable",
		"src/main.rs:5:9",
		"Finished dev",
	} {
		if !strings.Contains(got, key) {
			t.Errorf("关键信息应保留: %q\n输出:\n%s", key, got)
		}
	}
}

func TestRule_CargoTest_DropsPassingCases(t *testing.T) {
	input := strings.Join([]string{
		"   Compiling mycrate v0.1.0 (/workspace/mycrate)",
		"    Finished test [unoptimized + debuginfo] target(s) in 5.67s",
		"     Running unittests src/lib.rs (target/debug/deps/mycrate-abc)",
		"",
		"running 5 tests",
		"test util::tests::test_add ... ok",
		"test util::tests::test_sub ... ok",
		"test api::tests::test_get ... ok",
		"test api::tests::test_post ... FAILED",
		"test util::tests::test_mul ... ignored",
		"",
		"failures:",
		"",
		"---- api::tests::test_post stdout ----",
		"thread 'api::tests::test_post' panicked at 'expected 200, got 500'",
		"",
		"failures:",
		"    api::tests::test_post",
		"",
		"test result: FAILED. 3 passed; 1 failed; 1 ignored; 0 measured",
	}, "\n")

	got := runBuiltin(t, "cargo", []string{"test"}, input)

	for _, noise := range []string{
		"Compiling mycrate",
		"Finished test",
		"test util::tests::test_add ... ok",
		"test util::tests::test_sub ... ok",
		"test api::tests::test_get ... ok",
		"test util::tests::test_mul ... ignored",
	} {
		if strings.Contains(got, noise) {
			t.Errorf("噪音应被移除: %q\n输出:\n%s", noise, got)
		}
	}
	for _, key := range []string{
		"test api::tests::test_post ... FAILED",
		"thread 'api::tests::test_post' panicked",
		"test result: FAILED",
		"3 passed; 1 failed",
	} {
		if !strings.Contains(got, key) {
			t.Errorf("关键失败信息应保留: %q\n输出:\n%s", key, got)
		}
	}
}

func TestRule_CargoCheck_PreservesWarnings(t *testing.T) {
	input := strings.Join([]string{
		"    Updating crates.io index",
		"  Downloaded foo v1.0.0",
		"    Checking foo v1.0.0",
		"    Checking mycrate v0.1.0",
		"warning: unused import: `std::io`",
		"  --> src/main.rs:1:5",
		"    Finished dev [unoptimized + debuginfo] target(s) in 1.23s",
	}, "\n")

	got := runBuiltin(t, "cargo", []string{"check"}, input)

	if strings.Contains(got, "Checking foo v1.0.0") {
		t.Errorf("Checking 噪音应移除，输出:\n%s", got)
	}
	for _, key := range []string{"warning: unused import", "src/main.rs:1:5", "Finished dev"} {
		if !strings.Contains(got, key) {
			t.Errorf("关键信息应保留: %q\n输出:\n%s", key, got)
		}
	}
}
