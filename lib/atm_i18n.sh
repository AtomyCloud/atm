#!/usr/bin/env bash

atm_i18n_normalize() {
    local raw="${1:-}"

    raw="${raw%%.*}"
    raw="${raw%%@*}"
    raw="$(printf '%s' "$raw" | tr '[:upper:]_' '[:lower:]-')"

    case "$raw" in
        en|en-us|en-gb) printf 'en-us\n' ;;
        pt-br) printf 'pt-br\n' ;;
        pt|pt-pt) printf 'pt-pt\n' ;;
        zh|zh-cn|zh-hans|zh-sg) printf 'zh-cn\n' ;;
        es|es-es|es-mx|es-ar|es-cl|es-co) printf 'es\n' ;;
        *) printf '%s\n' "$raw" ;;
    esac
}

atm_i18n_available() {
    local file=""

    for file in "$ATM_LANG_DIR"/*.lang; do
        [[ -f "$file" ]] || continue
        basename "$file" .lang
    done | sort
}

atm_i18n_exists() {
    local locale="$1"
    [[ -f "$ATM_LANG_DIR/$locale.lang" ]]
}

atm_i18n_detect_system_lang() {
    local raw="${LC_ALL:-${LC_MESSAGES:-${LANG:-}}}"
    atm_i18n_normalize "$raw"
}

atm_i18n_prompt_initial() {
    local detected=""
    local choice=""
    local langs=()
    local idx=1
    local lang=""

    detected="$(atm_i18n_detect_system_lang)"

    printf 'ATM language is not configured.\n'

    if [[ -n "$detected" ]] && atm_i18n_exists "$detected"; then
        printf 'Detected language: %s\n' "$detected"
        printf 'Use this language? [Y/n]: '
        read -r choice

        case "$choice" in
            n|N|no|NO)
                ;;
            *)
                ATM_LANG="$detected"
                atm_config_set_key "$ATM_CONFIG_FILE" "ATM_LANG" "$ATM_LANG"
                return 0
                ;;
        esac
    fi

    printf '\nAvailable languages:\n'
    mapfile -t langs < <(atm_i18n_available)

    if ((${#langs[@]} == 0)); then
        atm_fail "No language files found in $ATM_LANG_DIR"
    fi

    for lang in "${langs[@]}"; do
        printf '%s) %s\n' "$idx" "$lang"
        idx=$((idx + 1))
    done

    printf 'Select initial language: '
    read -r choice

    if [[ "$choice" =~ ^[0-9]+$ ]] && ((choice >= 1 && choice <= ${#langs[@]})); then
        ATM_LANG="${langs[$((choice - 1))]}"
        atm_config_set_key "$ATM_CONFIG_FILE" "ATM_LANG" "$ATM_LANG"
        return 0
    fi

    if atm_i18n_exists "$choice"; then
        ATM_LANG="$choice"
        atm_config_set_key "$ATM_CONFIG_FILE" "ATM_LANG" "$ATM_LANG"
        return 0
    fi

    atm_fail "Invalid language selection."
}

atm_i18n_load_file() {
    local file="$1"

    [[ -f "$file" ]] || atm_fail "Language file not found: $file"

    # Trusted local file. Format: shell key=value.
    # shellcheck source=/dev/null
    source "$file"
}

atm_i18n_init() {
    ATM_LANG="${ATM_LANG:-}"

    if [[ -z "$ATM_LANG" && -f "$ATM_CONFIG_FILE" ]]; then
        # shellcheck source=/dev/null
        source "$ATM_CONFIG_FILE"
    fi

    if [[ -z "${ATM_LANG:-}" ]]; then
        if [[ -t 0 ]]; then
            atm_i18n_prompt_initial
        else
            atm_fail "ATM_LANG is not configured and no TTY is available. Run 'atm lang set <locale>'."
        fi
    fi

    ATM_LANG="$(atm_i18n_normalize "$ATM_LANG")"

    if ! atm_i18n_exists "$ATM_LANG"; then
        if [[ -t 0 ]]; then
            atm_warn "Configured language not found: $ATM_LANG"
            atm_i18n_prompt_initial
        else
            atm_fail "Configured language not found: $ATM_LANG"
        fi
    fi

    atm_i18n_load_file "$ATM_LANG_DIR/$ATM_LANG.lang"
}

atm_t() {
    local key="$1"
    local value=""

    value="${!key-}"

    if [[ -z "$value" ]]; then
        printf '[missing:%s]\n' "$key"
    else
        printf '%s\n' "$value"
    fi
}

atm_i18n_load_plugin_lang() {
    local plugin_id="$1"
    local plugin_lang_file="$ATM_PLUGIN_DIR/$plugin_id/lang/$ATM_LANG.lang"

    [[ -f "$plugin_lang_file" ]] || return 0
    atm_i18n_load_file "$plugin_lang_file"
}

atm_i18n_cli() {
    local subcmd="${1:-list}"

    case "$subcmd" in
        list)
            atm_i18n_available
            ;;
        set)
            local locale="${2:-}"

            [[ -n "$locale" ]] || atm_fail "Usage: atm lang set <locale>"

            locale="$(atm_i18n_normalize "$locale")"
            atm_i18n_exists "$locale" || atm_fail "Language not found: $locale"

            atm_config_set_key "$ATM_CONFIG_FILE" "ATM_LANG" "$locale"
            atm_success "Language set to: $locale"
            ;;
        *)
            atm_fail "Unknown lang command: $subcmd"
            ;;
    esac
}
