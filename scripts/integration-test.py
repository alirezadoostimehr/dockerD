#!/usr/bin/env python3
"""Exercise a dedicated DockerD test VM, including actual idle shutdown.

This creates temporary containers, one pod and one volume, changes the idle
timeout temporarily, and stops/restarts the supplied engine. Never point it at
an environment with work you need to keep running.
"""
import argparse
import http.client
import json
from pathlib import Path
import socket
import subprocess
import time
import urllib.parse
import urllib.request
import uuid

class UnixHTTP(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__('localhost', timeout=900)
        self.path = path
    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.path)

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--home', required=True, type=Path)
    parser.add_argument('--bundle', type=Path, default=Path('.runtime/bundle'))
    args = parser.parse_args()
    home = args.home.resolve()
    bundle = args.bundle.resolve()
    name = 'dockerd-test-' + uuid.uuid4().hex[:8]
    docker = [str(bundle/'docker/docker'), '--host', 'unix://' + str(home/'docker.sock')]
    kube = [str(bundle/'bin/kubectl'), '--kubeconfig', str(home/'kubeconfig')]
    def call(path, method='GET', body=None):
        client = UnixHTTP(str(home/'control.sock'))
        try:
            client.request(method, '/v1/'+path, body=json.dumps(body).encode() if body is not None else None, headers={'Content-Type':'application/json'})
            response = client.getresponse()
            result = json.load(response)
            if response.status >= 400:
                raise RuntimeError(result)
            return result
        finally:
            client.close()
    def run(command, *options):
        return subprocess.check_output(command + list(options), stderr=subprocess.STDOUT, text=True)
    def wait(label, predicate, timeout=180):
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            if predicate():
                print('PASS ' + label, flush=True)
                return
            time.sleep(2)
        raise AssertionError(label + ' timed out; status: ' + json.dumps(call('status')))
    def state():
        return call('status')
    def action(action, kind, identifier='', value='', **extra):
        return call('action', 'POST', dict(action=action, kind=kind, id=identifier, value=value, **extra))
    original = state()['settings']
    child = None
    forward = None
    try:
        if state()['state'] == 'stopped':
            call('start','POST')
        wait('engine ready', lambda: state()['state']=='active', 600)
        run(docker, 'pull', 'nginx:alpine')
        action('createVolume','volumes',value=name)
        action('run','images','nginx:alpine',name,localPort=18081,containerPort=80)
        wait('running container prevents idle',lambda:any(b['kind']=='container' and b['name']==name for b in state()['activity']['blockers']))
        wait('published container port',lambda:probe('http://127.0.0.1:18081'))
        run(kube,'run',name,'--image=nginx:alpine','--restart=Never')
        run(kube,'wait','--for=condition=Ready','pod/'+name,'--timeout=120s')
        forward=call('forwards','POST',dict(pod=name,namespace='default',localPort=18082,remotePort=80))['id']
        wait('pod port forward serves HTTP',lambda:probe('http://127.0.0.1:18082'))
        child=subprocess.Popen(kube+['exec',name,'--','sleep','60'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        wait('external exec and port forward tracked',lambda: {'exec','portforward'}.issubset({b['kind'] for b in state()['activity']['blockers']}))
        call('forwards/'+forward,'DELETE');forward=None
        child.terminate();child.wait(timeout=15);child=None
        run(kube,'delete','pod',name,'--wait=true','--timeout=60s')
        action('stop','containers',name)
        action('remove','containers',name)
        run(docker,'run','--rm','-v',name+':/data','nginx:alpine','sh','-c','printf persisted > /data/probe')
        wait('empty cluster becomes verifiably idle',lambda:state()['activity']['healthy'] and not state()['activity']['blockers'])
        settings=dict(original,idleEnabled=True,idleMinutes=1,countdownSeconds=10)
        call('settings','PUT',settings)
        call('idle/reset','POST')
        wait('idle countdown appears',lambda:state()['state']=='idle countdown',200)
        call('idle/reset','POST')
        assert state()['state']=='active', 'Keep running did not cancel countdown'
        print('PASS countdown cancellation',flush=True)
        wait('automatic VM shutdown',lambda:state()['state']=='stopped',150)
        result=subprocess.run(docker+['ps'],capture_output=True,text=True)
        assert result.returncode!=0 and 'Start' in result.stderr, result.stderr
        print('PASS stopped CLI fails without waking VM',flush=True)
        call('start','POST')
        wait('explicit restart',lambda:state()['state']=='active',600)
        assert run(docker,'run','--rm','-v',name+':/data','nginx:alpine','cat','/data/probe').strip()=='persisted'
        print('PASS volume data survives shutdown and restart',flush=True)
        print('PASS integration suite',flush=True)
    finally:
        if child is not None:
            child.terminate();child.wait(timeout=15)
        try:
            if forward:
                call('forwards/'+forward,'DELETE')
            if state()['state'] in ['active','idle countdown']:
                subprocess.run(kube+['delete','pod',name,'--ignore-not-found','--wait=false'],capture_output=True)
                subprocess.run(docker+['rm','-f',name],capture_output=True)
                subprocess.run(docker+['volume','rm',name],capture_output=True)
            call('settings','PUT',original)
        except Exception as error:
            print('Cleanup requires attention: '+str(error),flush=True)

def probe(url):
    try:
        with urllib.request.urlopen(url, timeout=2) as response:
            return response.status == 200
    except Exception:
        return False

if __name__=='__main__':
    main()
