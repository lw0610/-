"""Opt-in real model/REST/WS/QGIS replay; only uses an isolated runtime home."""
import argparse
import collections
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
import websocket

ROOT = Path(__file__).resolve().parents[3]


def redact(value):
    if isinstance(value, dict):
        return {k: ('[redacted]' if k.lower() in {'api_key', 'token', 'secret', 'password', 'authorization'} else redact(v)) for k, v in value.items()}
    if isinstance(value, list):
        return [redact(v) for v in value]
    if isinstance(value, str):
        return re.sub(r'([?&](?:tk|key|token|api_key)=)[^&\s"\']+', r'\1[redacted]', value, flags=re.I)
    return value


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--home', required=True, type=Path)
    p.add_argument('--agent', required=True, type=Path)
    p.add_argument('--qgis-python', default='/Applications/QGIS-LTR.app/Contents/MacOS/python')
    p.add_argument('--port', type=int, default=50081)
    p.add_argument('--name', default='hunan-rivers-rail')
    p.add_argument('--prompt', required=True)
    p.add_argument('--guide')
    p.add_argument('--project-path', type=Path, help='isolated QGIS project; defaults to HOME/work/project.qgz')
    p.add_argument('--resume-session', help='resume an existing isolated session through the normal plan start API')
    p.add_argument('--resume-resource-fingerprint', help='explicitly acknowledge the resource fingerprint returned by a failed isolated resume')
    p.add_argument('--approve-checkpoint', help='confirm one inspected checkpoint in the isolated session through the normal UI response')
    p.add_argument('--attachment', action='append', default=[])
    p.add_argument('--timeout', type=int, default=3600)
    p.add_argument('--approve-plan', action='store_true')
    p.add_argument('--approve-knowledge', action='store_true', help='confirm the unmodified real extraction only when it has no blocking errors')
    p.add_argument('--approve-step-completion', action='store_true', help='answer only a single continue choice titled 第N步完成确认; never accept data changes or failures')
    p.add_argument('--import-layers', type=Path, help='import actual local layers through the API before the task; preserve declared CRS')
    p.add_argument('--layer-name', action='append', default=[], help='select a scanned layer name; omit to confirm all draft entries')
    a = p.parse_args()
    home = a.home.resolve()
    if home == ROOT/'autocarto_data' or not (home/'config/config.yaml').is_file():
        p.error('use a prepared isolated runtime home, never real autocarto_data')
    out = home/'replays'/(a.name+'-'+time.strftime('%Y%m%d-%H%M%S'))
    out.mkdir(parents=True)
    env = dict(os.environ, AUTOCARTO_HOME=str(home), AUTOCARTO_PROJECT_ROOT=str(ROOT), AUTOCARTO_ASSETS_ROOT=str(ROOT/'autocarto'), QT_QPA_PLATFORM='offscreen', PYTHONDONTWRITEBYTECODE='1')
    base, project, sid = 'http://127.0.0.1:%d' % a.port, str((a.project_path or home/'work/project.qgz').resolve()), a.resume_session or str(uuid.uuid4())
    Path(project).parent.mkdir(parents=True, exist_ok=True)
    env['AUTOCARTO_REPLAY_PROJECT_PATH'] = project
    processes, streams = [], []
    started = time.monotonic()
    source_files = []
    for path in a.attachment:
        source = Path(path).resolve()
        if source.is_dir():
            source_files.extend(item for item in source.rglob('*') if item.is_file())
        else:
            source_files.append(source)
    if a.import_layers:
        source_files += [path for path in a.import_layers.resolve().rglob('*') if path.is_file()]
    before_hashes = {str(path): hashlib.sha256(path.read_bytes()).hexdigest() for path in source_files}
    summary = {'session_id': sid, 'project_path': project, 'name': a.name, 'prompt': a.prompt, 'real_model': True, 'real_qgis': True, 'status': 'starting'}
    (out/'request.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2))

    def api(method, path, data=None):
        request = urllib.request.Request(base+path, data=None if data is None else json.dumps(data, ensure_ascii=False).encode(), method=method, headers={'Content-Type': 'application/json'})
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                value = json.load(response)
        except urllib.error.HTTPError as exc:
            raise RuntimeError('HTTP %d: %s' % (exc.code, exc.read().decode())) from exc
        if not value.get('success'):
            raise RuntimeError(str(value))
        return value.get('data')

    try:
        ready_file = home/'worker-ready.json'
        ready_file.unlink(missing_ok=True)
        for name, command in [('agent', [str(a.agent.resolve()), '-port', str(a.port)]), ('qgis', [a.qgis_python, str(ROOT/'autocarto/tests/integration/flow_qgis_ws_worker.py')])]:
            log = (out/(name+'.log')).open('w')
            streams.append(log)
            processes.append(subprocess.Popen(command, cwd=Path(project).parent, env=env, stdout=log, stderr=subprocess.STDOUT))
            if name == 'agent':
                for _ in range(90):
                    if processes[-1].poll() is not None:
                        raise RuntimeError('agent exited; inspect agent.log')
                    pf = home/'data/.port'
                    if pf.exists() and pf.read_text().strip() == str(a.port):
                        break
                    time.sleep(1)
                else:
                    raise RuntimeError('agent did not publish port in 90 seconds')
        for _ in range(90):
            if processes[-1].poll() is not None:
                raise RuntimeError('QGIS exited; inspect qgis.log')
            if ready_file.is_file():
                break
            time.sleep(1)
        else:
            raise RuntimeError('QGIS tools were not ready in 90 seconds')
        if a.import_layers:
            draft = api('POST', '/api/layers/import/scan', {'path': str(a.import_layers.resolve()), 'pack': a.import_layers.name})
            (out/'layer-scan.json').write_text(json.dumps(redact(draft), ensure_ascii=False, indent=2))
            entries, skipped = [], []
            for entry in draft['entries']:
                manifest = entry['manifest']
                if a.layer_name and manifest['name'] not in a.layer_name:
                    skipped.append(manifest['name'])
                    continue
                if not manifest.get('declared_crs'):
                    raise RuntimeError('layer has no declared CRS and requires manual confirmation: '+manifest['name'])
                manifest['crs'] = manifest['declared_crs']
                manifest['compliance'] = '真实回放沿用文件声明坐标系；实际地理基准尚待人工核实。原始数据只读。'
                entries.append(manifest)
            if a.layer_name and set(a.layer_name) != {item['name'] for item in entries}:
                raise RuntimeError('selected layer names are missing from the scan')
            summary['skipped_import_layers'] = skipped
            imported = api('POST', '/api/layers/import/confirm', {'pack': a.import_layers.name, 'entries': entries})
            (out/'layer-import.json').write_text(json.dumps(redact(imported), ensure_ascii=False, indent=2))
            print(json.dumps({'action': 'import_layers', 'count': len(imported['items']), 'crs': 'declared; not independently confirmed'}, ensure_ascii=False), flush=True)
        query = '?'+urllib.parse.urlencode({'project_path': project})
        if not a.resume_session:
            api('POST', '/api/sessions'+query, {'session_id': sid, 'name': 'D51 '+a.name, 'project_path': project})
        else:
            api('GET', '/api/sessions/%s%s' % (sid, query))
        connection = websocket.create_connection('ws://127.0.0.1:%d/ws?session_id=%s' % (a.port, sid), origin='http://localhost:3000', timeout=2)
        connection.send(json.dumps({'type': 'instance_select', 'data': {'instance_id': 't53-production-qgis'}}))
        last_seq = 0
        if a.resume_session:
            for previous_events in (home/'replays').glob('*/events.jsonl'):
                with previous_events.open() as lines:
                    for line in lines:
                        item = json.loads(line)
                        if item.get('type') == 'run_event' and item.get('session_id') == sid:
                            last_seq = max(last_seq, item['data'].get('seq', 0))
        connection.send(json.dumps({'type': 'session_attach', 'session_id': sid, 'data': {'session_id': sid, 'last_seen_seq': last_seq}}))
        rid = 'd51-'+uuid.uuid4().hex
        data = {'project_path': project, 'instance_id': 't53-production-qgis', 'mode': 'build', 'run_intent': 'execute'}
        if a.guide:
            data['guide_id'] = a.guide
        if a.attachment:
            data['attachments'] = [{'path': str(Path(path).resolve()), 'kind': 'folder' if Path(path).is_dir() else 'file'} for path in a.attachment]
        if a.resume_session:
            previous = api('GET', '/api/sessions/%s/plans%s' % (sid, query))['plans']
            snapshot = previous[-1]
            current = snapshot['plan']
            action = 'failure' if snapshot['status'] == 'needs_replan' else 'start'
            body = {'version': current['version'], 'resume_run_id': snapshot['flow_run_id']}
            if a.resume_resource_fingerprint:
                body.update(resume_resource_choice='continue_snapshot', resource_fingerprint=a.resume_resource_fingerprint)
                summary['resource_confirmation'] = dict(body)
            if action == 'failure':
                body['action'] = 'replan'
            api('POST', '/api/sessions/%s/plans/%s/%s%s' % (sid, current['plan_id'], action, query), body)
        elif not a.resume_session:
            connection.send(json.dumps({'type': 'task', 'session_id': sid, 'request_id': rid, 'idempotency_key': rid, 'content': a.prompt, 'data': data}, ensure_ascii=False))
        summary['status'] = 'running'
        print(json.dumps({'status': 'started', 'session_id': sid, 'evidence': str(out)}, ensure_ascii=False), flush=True)
        counts, confirmed, latest, running = collections.Counter(), set(), None, True
        knowledge_checkpoints, awaiting_resume = set(), False
        with (out/'events.jsonl').open('w') as events:
            while time.monotonic()-started < a.timeout:
                try:
                    event = redact(json.loads(connection.recv()))
                except websocket.WebSocketTimeoutException:
                    continue
                events.write(json.dumps(event, ensure_ascii=False)+'\n')
                events.flush()
                if event.get('session_id') not in (None, '', sid):
                    continue
                kind, payload = event.get('type', ''), event.get('data') or {}
                if kind == 'run_event':
                    kind, payload = payload.get('source_type', kind), payload.get('payload') or {}
                counts[kind] += 1
                if kind in ('plan_event', 'flow_event', 'error', 'interrupt_request') and payload.get('event') not in ('flow.acquisition_progress', 'flow.budget'):
                    print(json.dumps({'type': kind, 'status': payload.get('status'), 'event': payload.get('event'), 'content': str(event.get('content', ''))[:1000]}, ensure_ascii=False), flush=True)
                if kind == 'plan_event':
                    latest = payload
                    (out/'plan.json').write_text(json.dumps(payload, ensure_ascii=False, indent=2))
                    identity = payload.get('plan_id'), payload.get('plan_version')
                    if a.approve_plan and payload.get('status') == 'awaiting_confirmation' and identity not in confirmed:
                        confirmed.add(identity)
                        result = api('POST', '/api/sessions/%s/plans/%s/start%s' % (sid, identity[0], query), {'version': identity[1]})
                        print(json.dumps({'action': 'confirm_plan', 'plan_id': identity[0], 'status': result.get('status')}, ensure_ascii=False), flush=True)
                if kind == 'interrupt_request' and a.approve_checkpoint == payload.get('checkpoint_id'):
                    if a.approve_checkpoint not in confirmed:
                        confirmed.add(a.approve_checkpoint)
                        connection.send(json.dumps({'type': 'interrupt_response', 'session_id': sid,
                                                    'data': {'checkpoint_id': a.approve_checkpoint, 'approved': True, 'user_data': {'confirmed': True}}}, ensure_ascii=False))
                        summary['explicit_checkpoint_confirmation'] = a.approve_checkpoint
                        awaiting_resume = True
                    continue
                if kind == 'interrupt_request' and a.approve_knowledge:
                    checkpoint = payload.get('checkpoint_id')
                    if checkpoint in knowledge_checkpoints:
                        continue
                    reviews = api('GET', '/api/sessions/%s/knowledge-reviews%s' % (sid, query))
                    review = next((item for item in reviews if item.get('status') == 'awaiting_review' and not item.get('errors')), None)
                    if review:
                        review_id = review['review_id']
                        (out/('knowledge-'+review_id+'.json')).write_text(json.dumps(redact(review), ensure_ascii=False, indent=2))
                        accepted = api('POST', '/api/sessions/%s/knowledge-reviews/%s/confirm%s' % (sid, review_id, query), {'base_revision': review['revision'], 'request_id': 'd51-'+uuid.uuid4().hex})
                        knowledge_checkpoints.add(checkpoint)
                        awaiting_resume = True
                        summary.setdefault('knowledge_confirmations', []).append({'review_id': review_id, 'revision': review['revision'], 'status': accepted['status'], 'graph_modified': False, 'semantic_review': 'not manually verified'})
                        print(json.dumps({'action': 'confirm_knowledge', 'review_id': review_id, 'status': accepted['status'], 'graph_modified': False}, ensure_ascii=False), flush=True)
                        continue
                if kind == 'interrupt_request' and a.approve_step_completion:
                    contexts = payload.get('interrupt_contexts') or []
                    info = contexts[0].get('info', {}) if len(contexts) == 1 else {}
                    options = info.get('options') or []
                    completion_title = re.fullmatch(r'第[0-9一二三四五六七八九十]+步完成确认', info.get('title', ''))
                    completion_description = info.get('title') == '确认' and re.fullmatch(r'第[0-9]+步（[^）]+）已完成，请确认后继续。', info.get('description', ''))
                    if (info.get('type') == 'plan_confirmation' and (completion_title or completion_description) and
                            info.get('select_mode') == 'confirm' and len(options) == 1 and
                            options[0].get('value') == 'continue'):
                        checkpoint = payload['checkpoint_id']
                        if checkpoint in confirmed:
                            continue
                        confirmed.add(checkpoint)
                        connection.send(json.dumps({'type': 'interrupt_response', 'session_id': sid,
                                                    'data': {'checkpoint_id': checkpoint, 'approved': True, 'user_data': {'confirmed': True}}}, ensure_ascii=False))
                        awaiting_resume = True
                        summary.setdefault('step_confirmations', []).append({'checkpoint_id': checkpoint, 'info': info, 'selected': 'continue'})
                        print(json.dumps({'action': 'confirm_step_completion', 'checkpoint_id': checkpoint, 'title': info['title']}, ensure_ascii=False), flush=True)
                        continue
                if kind == 'task_running_changed':
                    running = bool(payload.get('running'))
                    if running:
                        awaiting_resume = False
                if kind in ('error', 'interrupt_request'):
                    summary.update(status='error' if kind == 'error' else 'awaiting_interaction', detail=event)
                    break
                if latest and latest.get('status') in ('completed', 'completed_with_issues', 'failed', 'cancelled', 'needs_replan', 'waiting_budget', 'interrupted'):
                    summary['status'] = latest['status']
                    if not running or latest['status'] in ('waiting_budget', 'interrupted'):
                        break
                if kind == 'task_running_changed' and not running and not latest and not awaiting_resume:
                    summary['status'] = 'turn_finished_without_plan'
                    break
            else:
                summary['status'] = 'timeout'
        connection.close()
        summary.update(event_counts=dict(counts), plan=latest, model_call_count=None,
                       model_call_note='derive from persisted model traces; messages and tools are not model calls')
        summary['plans'] = api('GET', '/api/sessions/%s/plans%s' % (sid, query))
    except KeyboardInterrupt:
        summary.update(status="stopped_for_diagnosis")
    except Exception as exc:
        summary.update(status='harness_or_runtime_error', error=str(exc))
        print(json.dumps({'error': str(exc), 'evidence': str(out)}, ensure_ascii=False), flush=True)
    finally:
        summary['inputs_unchanged'] = all(path.is_file() and hashlib.sha256(path.read_bytes()).hexdigest() == before_hashes[str(path)] for path in source_files)
        summary['input_hashes'] = before_hashes
        summary['elapsed_seconds'] = round(time.monotonic()-started, 2)
        (out/'summary.json').write_text(json.dumps(redact(summary), ensure_ascii=False, indent=2))
        for process in reversed(processes):
            process.terminate()
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        for stream in streams:
            stream.close()
    print(json.dumps({'status': summary['status'], 'elapsed_seconds': summary['elapsed_seconds'], 'evidence': str(out)}, ensure_ascii=False), flush=True)
    return 0 if summary['status'] == 'completed' else 1


if __name__ == '__main__':
    raise SystemExit(main())
