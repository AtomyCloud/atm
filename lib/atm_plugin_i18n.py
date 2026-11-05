"""Read literal plugin locale assignments without executing shell code."""

import os
from pathlib import Path
import re
import shlex


LOCALES = ("en-us", "pt-br", "pt-pt", "es", "it", "fr", "de", "ru", "ja", "zh-cn", "ko")
KEY = re.compile(r"ATM_PLUGIN_[A-Z0-9_]+\Z")


def normalize(locale):
    value = locale.split(".", 1)[0].split("@", 1)[0].lower().replace("_", "-")
    aliases = {"en": "en-us", "en-gb": "en-us", "pt": "pt-pt", "zh": "zh-cn",
               "zh-hans": "zh-cn", "zh-sg": "zh-cn", "es-es": "es", "es-mx": "es",
               "es-ar": "es", "es-cl": "es", "es-co": "es"}
    value = aliases.get(value, value)
    return value if value in LOCALES else "en-us"


def read_catalog(path):
    """Supported format: one literal, quoted key=value per line, plus comments."""
    result = {}
    for number, line in enumerate(Path(path).read_text(encoding="utf-8").splitlines(), 1):
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        key, separator, value = line.strip().partition("=")
        if not separator or not KEY.fullmatch(key) or key in result:
            raise ValueError(f"Invalid or duplicate locale key: {path}:{number}")
        words = shlex.split(value, comments=True, posix=True)
        if len(words) != 1:
            raise ValueError(f"Expected one literal locale value: {path}:{number}")
        text = words[0]
        if value.startswith('"'):
            text = text.replace(r"\$", "$").replace(r"\`", "`")
        result[key] = text
    return result


def translator(plugin_file):
    directory = Path(plugin_file).resolve().parent / "lang"
    english = read_catalog(directory / "en-us.lang")
    messages = dict(english)
    locale = normalize(os.environ.get("ATM_LANG", "en-us"))
    if locale != "en-us":
        try:
            messages.update(read_catalog(directory / (locale + ".lang")))
        except (OSError, ValueError):
            # An unavailable/malformed optional locale must not break a helper.
            pass

    def translate(key, **values):
        text = messages.get(key) or english.get(key) or f"[missing:{key}]"
        if not values:
            return text
        try:
            return text.format(**values)
        except (KeyError, ValueError, IndexError):
            return english.get(key, f"[missing:{key}]").format(**values)

    return translate
