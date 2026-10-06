"""Verify server-issued review provenance without changing the extraction schema.

The model-facing compiler must not accept a caller's ``trusted=True`` flag. The
review service signs the exact source and graph using an application-owned key;
only verified records may omit model confidence and original-text evidence.
"""

from __future__ import annotations

import hashlib
import hmac
import json
from dataclasses import dataclass
from typing import Any, Mapping

from ..core.paths import get_autocarto_home


REVIEW_COLLECTION_IDS = {
    "actors": "actor_id",
    "spatial_entities": "entity_id",
    "events": "event_id",
    "routes": "route_id",
    "point_presentations": "presentation_id",
    "callouts": "callout_id",
}


class ReviewContextError(ValueError):
    """A supplied review context is not an authenticated graph revision."""


@dataclass(frozen=True)
class ReviewedRecord:
    collection: str
    id: str
    origin: str


@dataclass(frozen=True)
class ReviewedIssueDecision:
    issue_id: str
    action: str
    explanation: str
    subject_refs: tuple[str, ...]
    revision: int

    def to_dict(self) -> dict[str, Any]:
        return {
            "issue_id": self.issue_id, "action": self.action,
            "explanation": self.explanation, "subject_refs": list(self.subject_refs),
            "revision": self.revision, "source": "human_review",
        }


@dataclass(frozen=True)
class ReviewedProvenance:
    review_id: str
    revision: int
    records: tuple[ReviewedRecord, ...]
    issue_resolutions: tuple[ReviewedIssueDecision, ...]

    def to_dict(self) -> dict[str, Any]:
        return {
            "review_id": self.review_id, "revision": self.revision,
            "manual_records": [
                {"collection": item.collection, "id": item.id, "origin": item.origin}
                for item in self.records
            ],
            "issue_resolutions": [item.to_dict() for item in self.issue_resolutions],
        }


@dataclass(frozen=True)
class VerifiedReview:
    review_id: str
    revision: int
    source_sha256: str
    graph_sha256: str
    records: tuple[ReviewedRecord, ...]
    issue_resolutions: tuple[ReviewedIssueDecision, ...] = ()
    inherited_provenance: tuple[ReviewedProvenance, ...] = ()
    source_graph: Mapping[str, Any] | None = None
    rendering_adaptations: tuple[dict[str, Any], ...] = ()

    def allows(self, collection: str, record_id: Any) -> bool:
        return any(
            item.collection == collection and item.id == record_id
            for item in self.records
        )

    def object_paths(self, graph: Mapping[str, Any]) -> frozenset[str]:
        paths = set()
        for collection, id_key in REVIEW_COLLECTION_IDS.items():
            values = graph.get(collection)
            if not isinstance(values, list):
                continue
            for index, item in enumerate(values):
                if isinstance(item, Mapping) and self.allows(
                    collection, item.get(id_key)
                ):
                    paths.add(f"{collection}[{index}]")
        return frozenset(paths)

    def to_dict(self) -> dict[str, Any]:
        return {
            "version": 1,
            "review_id": self.review_id,
            "revision": self.revision,
            "source_sha256": self.source_sha256,
            "graph_sha256": self.graph_sha256,
            "manual_records": [
                {"collection": item.collection, "id": item.id, "origin": item.origin}
                for item in self.records
            ],
            "issue_resolutions": [item.to_dict() for item in self.issue_resolutions],
            "inherited_provenance": [item.to_dict() for item in self.inherited_provenance],
            "rendering_adaptations": list(self.rendering_adaptations),
        }


def _canonical_graph(graph: Any) -> str:
    return json.dumps(
        graph,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
        allow_nan=False,
    )


def verify_review_context(
    token: Any,
    graph: Mapping[str, Any],
    source_text: str,
) -> VerifiedReview:
    """Authenticate a review token. Key paths can never be supplied by callers.

    ``payload`` is a JSON string signed as its original UTF-8 bytes, not a
    reserialized object. This avoids cross-language JSON canonicalization in
    the Go/Python signature protocol. The key is 32 random bytes encoded as
    64 hexadecimal characters in the application data directory.
    """

    if not isinstance(token, Mapping):
        raise ReviewContextError("核对凭据必须是对象")
    payload_text = token.get("payload")
    signature = token.get("signature")
    if not isinstance(payload_text, str) or not isinstance(signature, str):
        raise ReviewContextError("核对凭据缺少 payload 或 signature")
    if len(signature) != 64 or any(
        char not in "0123456789abcdef" for char in signature
    ):
        raise ReviewContextError("核对凭据签名格式无效")
    try:
        key_path = (
            get_autocarto_home() / "data" / "knowledge_reviews" / ".signing_key"
        )
        key = bytes.fromhex(key_path.read_text(encoding="ascii").strip())
    except (OSError, ValueError, UnicodeError) as exc:
        raise ReviewContextError("无法读取本地核对签名密钥，请检查应用数据目录") from exc
    if len(key) != 32:
        raise ReviewContextError("本地核对签名密钥格式无效")
    expected = hmac.new(
        key, payload_text.encode("utf-8"), hashlib.sha256
    ).hexdigest()
    if not hmac.compare_digest(expected, signature):
        raise ReviewContextError("核对凭据签名无效")
    try:
        payload = json.loads(payload_text)
    except (TypeError, ValueError) as exc:
        raise ReviewContextError("核对凭据内容不是有效 JSON") from exc
    if (
        not isinstance(payload, dict)
        or type(payload.get("version")) is not int
        or payload["version"] != 1
    ):
        raise ReviewContextError("不支持的核对凭据版本")
    review_id = payload.get("review_id")
    revision = payload.get("revision")
    if (
        not isinstance(review_id, str)
        or not review_id.strip()
        or type(revision) is not int
        or revision < 1
    ):
        raise ReviewContextError("核对凭据缺少有效的记录编号或版本")
    source_sha256 = hashlib.sha256(source_text.encode("utf-8")).hexdigest()
    if payload.get("source_sha256") != source_sha256:
        raise ReviewContextError("核对凭据与本次原文不一致")
    graph_text = payload.get("graph_json")
    try:
        if not isinstance(graph_text, str):
            raise ValueError("graph_json must be a string")
        signed_graph = json.loads(graph_text)
        if _canonical_graph(signed_graph) != _canonical_graph(graph):
            raise ValueError("graph mismatch")
    except (TypeError, ValueError) as exc:
        raise ReviewContextError("核对凭据与本次图谱内容不一致") from exc

    source_graph = None
    if "source_graph_json" in payload:
        try:
            source_graph = json.loads(payload["source_graph_json"])
            if not isinstance(source_graph, dict):
                raise ValueError("source graph must be an object")
        except (TypeError, ValueError) as exc:
            raise ReviewContextError("已接受的原始图谱格式无效") from exc
    adaptations = payload.get("rendering_adaptations", [])
    if not isinstance(adaptations, list) or any(not isinstance(item, dict) for item in adaptations):
        raise ReviewContextError("自动制图适配记录必须为对象数组")

    records = _parse_manual_records(payload.get("manual_records"), graph)
    decisions = _parse_issue_decisions(payload.get("issue_resolutions", []), revision)
    raw_history = payload.get("inherited_provenance", [])
    if not isinstance(raw_history, list):
        raise ReviewContextError("继承核对来源必须为数组")
    history, seen_history = [], set()
    for item in raw_history:
        if (
            not isinstance(item, dict)
            or not isinstance(item.get("review_id"), str)
            or not item["review_id"].strip()
            or type(item.get("revision")) is not int
            or item["revision"] < 1
        ):
            raise ReviewContextError("继承核对来源缺少有效记录编号或版本")
        identity = (item["review_id"], item["revision"])
        if identity in seen_history:
            raise ReviewContextError("继承核对来源重复")
        seen_history.add(identity)
        history.append(ReviewedProvenance(
            review_id=item["review_id"], revision=item["revision"],
            # Historical objects may have been removed in a later revision.
            # Only the current signed manual_records grant validation privileges.
            records=tuple(_parse_manual_records(item.get("manual_records", []))),
            issue_resolutions=tuple(_parse_issue_decisions(
                item.get("issue_resolutions", []), item["revision"],
            )),
        ))
    return VerifiedReview(
        review_id=review_id,
        revision=revision,
        source_sha256=source_sha256,
        graph_sha256=hashlib.sha256(graph_text.encode("utf-8")).hexdigest(),
        records=tuple(records),
        issue_resolutions=tuple(decisions),
        inherited_provenance=tuple(history),
        source_graph=source_graph,
        rendering_adaptations=tuple(adaptations),
    )


def _parse_manual_records(raw_records: Any, graph: Mapping[str, Any] | None = None) -> list[ReviewedRecord]:
    if not isinstance(raw_records, list):
        raise ReviewContextError("核对凭据缺少人工修改来源")
    records = []
    seen = set()
    for item in raw_records:
        if not isinstance(item, dict):
            raise ReviewContextError("人工修改来源格式无效")
        collection = item.get("collection")
        record_id = item.get("id")
        origin = item.get("origin")
        if (
            not isinstance(collection, str)
            or collection not in REVIEW_COLLECTION_IDS
            or not isinstance(record_id, str)
            or not record_id
            or not isinstance(origin, str)
            or origin not in {"manual_added", "manual_edited"}
        ):
            raise ReviewContextError("人工修改来源字段无效")
        identity = (collection, record_id)
        if identity in seen:
            raise ReviewContextError("人工修改来源重复")
        seen.add(identity)
        if graph is not None:
            values = graph.get(collection)
            if not isinstance(values, list) or not any(
                isinstance(value, dict)
                and value.get(REVIEW_COLLECTION_IDS[collection]) == record_id
                for value in values
            ):
                raise ReviewContextError("人工修改来源引用了不存在的对象")
        records.append(ReviewedRecord(collection, record_id, origin))
    return records


def _parse_issue_decisions(raw_decisions: Any, revision: int) -> list[ReviewedIssueDecision]:
    if not isinstance(raw_decisions, list):
        raise ReviewContextError("人工问题处理记录必须为数组")
    decisions = []
    decision_ids = set()
    for item in raw_decisions:
        if (
            not isinstance(item, dict)
            or not isinstance(item.get("issue_id"), str)
            or not item["issue_id"]
            or item["issue_id"] in decision_ids
            or not isinstance(item.get("action"), str)
            or item.get("action") not in {"edited", "excluded", "confirmed"}
            or item.get("source") != "human_review"
            or not isinstance(item.get("explanation"), str)
            or not item["explanation"].strip()
            or type(item.get("revision")) is not int
            or not 1 <= item["revision"] <= revision
            or not isinstance(item.get("subject_refs"), list)
            or not item["subject_refs"]
            or any(not isinstance(ref, str) or not ref for ref in item["subject_refs"])
        ):
            raise ReviewContextError("人工问题处理记录不完整或重复")
        decision_ids.add(item["issue_id"])
        decisions.append(ReviewedIssueDecision(
            issue_id=item["issue_id"], action=item["action"],
            explanation=item["explanation"], subject_refs=tuple(item["subject_refs"]),
            revision=item["revision"],
        ))
    return decisions
