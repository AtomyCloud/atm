#!/usr/bin/env bash

atm_config_init() {
    local env_atm_lang="${ATM_LANG:-}"
    local env_atm_apps_dir="${ATM_APPS_DIR:-}"
    local env_atm_cache_dir="${ATM_CACHE_DIR:-}"
    local env_atm_state_dir="${ATM_STATE_DIR:-}"

    if [[ ! -f "$ATM_CONFIG_FILE" ]]; then
        atm_debug "Creating config file: $ATM_CONFIG_FILE"

        if [[ "${ATM_DRY_RUN:-0}" != "1" ]]; then
            cat > "$ATM_CONFIG_FILE" <<EOF
# ATM - Atomy Tools Modules configuration
ATM_VERSION="$ATM_VERSION"
ATM_HOME="$ATM_HOME"
ATM_APPS_DIR="$ATM_APPS_DIR"
ATM_CACHE_DIR="$ATM_CACHE_DIR"
ATM_STATE_DIR="$ATM_STATE_DIR"
ATM_LANG=""
ATM_REPO_URL="$ATM_REPO_URL"
ATM_SELF_UPDATE_BRANCH="main"
ATM_SELF_UPDATE_MODE="git"
EOF
        fi
    fi

    if [[ ! -f "$ATM_PLUGINS_CONFIG_FILE" ]]; then
        atm_debug "Creating plugins config file: $ATM_PLUGINS_CONFIG_FILE"

        if [[ "${ATM_DRY_RUN:-0}" != "1" ]]; then
            cat > "$ATM_PLUGINS_CONFIG_FILE" <<'EOF'
# ATM plugin enable/disable configuration
# 1 = enabled, 0 = disabled
ATM_PLUGIN_GO_ENABLED="1"
EOF
        fi
    fi

    # shellcheck source=/dev/null
    [[ -f "$ATM_CONFIG_FILE" ]] && source "$ATM_CONFIG_FILE"

    # shellcheck source=/dev/null
    [[ -f "$ATM_PLUGINS_CONFIG_FILE" ]] && source "$ATM_PLUGINS_CONFIG_FILE"

    # Environment variables must override config file values.
    [[ -n "$env_atm_lang" ]] && ATM_LANG="$env_atm_lang"
    [[ -n "$env_atm_apps_dir" ]] && ATM_APPS_DIR="$env_atm_apps_dir"
    [[ -n "$env_atm_cache_dir" ]] && ATM_CACHE_DIR="$env_atm_cache_dir"
    [[ -n "$env_atm_state_dir" ]] && ATM_STATE_DIR="$env_atm_state_dir"
}

atm_config_set_key() {
    local file="$1"
    local key="$2"
    local value="$3"
    local tmp=""

    [[ -n "$key" ]] || atm_fail "Config key is required."

    if [[ "${ATM_DRY_RUN:-0}" == "1" ]]; then
        printf 'DRY-RUN: set %s=%q in %s\n' "$key" "$value" "$file"
        return 0
    fi

    mkdir -p "$(dirname "$file")"
    tmp="$(mktemp)"

    if [[ -f "$file" ]]; then
        awk -v key="$key" '
            BEGIN { replaced=0 }
            $0 ~ "^" key "=" {
                if (replaced == 0) {
                    print "__ATM_REPLACE__"
                    replaced=1
                }
                next
            }
            { print }
            END {
                if (replaced == 0) {
                    print "__ATM_REPLACE__"
                }
            }
        ' "$file" > "$tmp"
    else
        printf '__ATM_REPLACE__\n' > "$tmp"
    fi

    sed "s|__ATM_REPLACE__|$key=\"$(printf '%s' "$value" | sed 's/[\/&]/\\&/g')\"|" "$tmp" > "${tmp}.new"
    mv "${tmp}.new" "$file"
    rm -f "$tmp"
}