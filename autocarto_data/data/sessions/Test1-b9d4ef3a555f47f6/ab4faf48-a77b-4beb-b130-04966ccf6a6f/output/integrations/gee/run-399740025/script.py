import os, json, glob

# 1. 可用模块
for mod in ['googleapiclient', 'google.auth', 'google.oauth2.credentials', 'google_auth_oauthlib']:
    try:
        __import__(mod)
        print('MODULE OK:', mod)
    except Exception as e:
        print('MODULE MISSING:', mod, '|', e)

# 2. EE 凭据文件位置与作用域
cred_candidates = [
    os.path.expanduser('~/.config/earthengine/credentials'),
    os.path.expanduser('~/.config/earthengine/credentials.db'),
]
for p in cred_candidates:
    if os.path.exists(p):
        print('CRED FILE:', p)
        try:
            with open(p) as f:
                data = json.load(f)
            keys = list(data.keys())
            print('CRED KEYS:', keys)
            for k in ['client_id', 'client_secret', 'scopes', 'scope', 'refresh_token', 'type', 'project_id']:
                if k in data:
                    v = data[k]
                    if k in ('client_secret', 'refresh_token', 'access_token'):
                        v = '<redacted>'
                    print('  ', k, '=', v)
        except Exception as e:
            print('CRED PARSE ERR:', e)

# 3. ee 内部凭据
try:
    import ee
    cred = ee.data.getAssetRootQuota  # not helpful
    print('ee data API ok')
except Exception as e:
    print('ee err', e)

# 4. 检查是否有 drive 相关辅助
import inspect
mods = [n for n in dir() if not n.startswith('_')]
print('GLOBALS:', mods)
