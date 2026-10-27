"""Redact credentials at display/log boundaries, never in a live provider URI."""
import copy
import logging
import re

_KEYS = r"tk|token|access_token|api_key|apikey|password|passwd|secret|client_secret"
_QUERY = re.compile(r"((?:[?&;]|%3f|%26|%3b|%253f|%2526|%253b)(?:" + _KEYS + r")(?:=|%3d|%253d))(?:(?!&|%26|%2526|[\s'\"<>]).)+", re.I)
_ASSIGN = re.compile(r"(\b(?:" + _KEYS + r")[\"']?\s*[:=]\s*[\"']?)([^\s&'\"<>,}]+)", re.I)
_PRIVATE = re.compile(r"([\"']?__private_credentials[\"']?\s*:\s*)\{[^}]*\}", re.I)
_USERINFO = re.compile(r"(https?://)[^/\s:@]+:[^/\s@]+@", re.I)
_SECRET_KEYS = set(_KEYS.split("|")) | {"__private_credentials"}


def redact_sensitive_text(value):
    text = str(value or "")
    text = _PRIVATE.sub(r"\1[redacted]", text)
    text = _QUERY.sub(r"\1[redacted]", text)
    text = _ASSIGN.sub(r"\1[redacted]", text)
    return _USERINFO.sub(r"\1[redacted]@", text)


def redact_sensitive(value):
    if isinstance(value, str):
        return redact_sensitive_text(value)
    if isinstance(value, dict):
        return {key: "[redacted]" if str(key).lower() in _SECRET_KEYS else redact_sensitive(child) for key, child in value.items()}
    if isinstance(value, list):
        return [redact_sensitive(child) for child in value]
    if isinstance(value, tuple):
        return tuple(redact_sensitive(child) for child in value)
    return value


class RedactingFormatter(logging.Formatter):
    def format(self, record):
        safe = copy.copy(record)
        safe.msg = redact_sensitive_text(safe.msg)
        safe.args = redact_sensitive(safe.args)
        return redact_sensitive_text(super().format(safe))
