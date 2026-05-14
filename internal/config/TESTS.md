# internal/config — Test Scenarios

## TestLoadDefaults
**Scenario:** Load from an empty TOML file.
**Setup:** Write an empty file to a temp directory; call `Load(path)`.
**Assertions:** All struct fields match `Default()` values; no zero-value strings returned.

## TestLoadOverrides
**Scenario:** Load a TOML file with partial overrides.
**Setup:** Write a TOML file that sets only `[db] path = "/custom/path.db"`.
**Assertions:** `cfg.DB.Path == "/custom/path.db"`; all other fields match defaults.

## TestLoadInvalid
**Scenario:** Load a file with invalid TOML syntax.
**Setup:** Write `"key = [invalid"` to a temp file; call `Load(path)`.
**Assertions:** Error is returned; result is nil.

## TestLoadMissing
**Scenario:** Load from a non-existent path.
**Setup:** Use a temp directory path that does not exist.
**Assertions:** Error is returned; result is nil.

## TestConfigPath
**Scenario:** Verify canonical config path.
**Setup:** Call `ConfigPath()`.
**Assertions:** Returned path contains `.lth/config.toml`; no error.

## TestDefault
**Scenario:** Verify Default() returns sensible values.
**Setup:** Call `Default()`.
**Assertions:**
- `DB.Path` non-empty and contains `.lth`
- `Embedding.BaseURL` == `"http://localhost:11434"`
- `Embedding.Model` == `"nomic-embed-text"`
- `Embedding.TimeoutS` == 30
- `LLM.Model` non-empty
- `Compaction.L5Threshold` == 50
- `Search.DefaultTopK` == 10
- `Search.Alpha` > 0
