import json
from mitmproxy import http

def request(flow: http.HTTPFlow):
    if 'codebuff.com' in flow.request.pretty_host or 'freebuff' in flow.request.pretty_host:
        print(f'\n=== REQUEST to {flow.request.url} ===')
        print(f'Method: {flow.request.method}')
        print('Headers:')
        for k, v in flow.request.headers.items():
            print(f'  {k}: {v}')
        if flow.request.content:
            try:
                body = json.loads(flow.request.content)
                print(f'Body: {json.dumps(body, indent=2)[:3000]}')
            except:
                print(f'Body (raw): {flow.request.content[:1000]}')

def response(flow: http.HTTPFlow):
    if 'codebuff.com' in flow.request.pretty_host or 'freebuff' in flow.request.pretty_host:
        print(f'\n=== RESPONSE from {flow.request.url} ===')
        print(f'Status: {flow.response.status_code}')
        print('Headers:')
        for k, v in flow.response.headers.items():
            print(f'  {k}: {v}')
        if flow.response.content:
            print(f'Body: {flow.response.content[:2000]}')
