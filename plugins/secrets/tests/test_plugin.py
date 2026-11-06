"""Secrets lifecycle in disposable homes with a mock Flatpak; never opens a vault."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

REPO = Path(__file__).resolve().parents[3]
PRELUDE = r'''
set -Eeuo pipefail
source "$ATM_HOME/lib/atm_core.sh"
source "$ATM_HOME/lib/atm_i18n.sh"
source "$ATM_HOME/lib/atm_manifest.sh"
source "$ATM_HOME/lang/en-us.lang"
source "$ATM_HOME/plugins/secrets/plugin.conf"
source "$ATM_HOME/plugins/secrets/plugin.sh"
source "$ATM_HOME/plugins/secrets/lang/en-us.lang"
atm_prompt_continue() { :; }
'''
MOCK = r'''#!/usr/bin/env python3
import json, os, pathlib, sys
root=pathlib.Path(os.environ['TEST_ROOT'])
args=sys.argv[1:]
assert os.environ['FLATPAK_USER_DIR']==os.environ['EXPECTED_FLATPAK'], os.environ['FLATPAK_USER_DIR']
assert '--user' in args and '--system' not in args
assert not {'--delete-data','--all','--unused','--force-remove','--no-gpg-verify'} & set(args)
with (root/'calls').open('a') as log: log.write(json.dumps(args)+'\n')
path=root/'state.json'
state=json.loads(path.read_text())
action=args[0]
if os.environ.get('FAIL')==action: sys.exit(9)
if action=='list':
    if state.get('bad_query'): sys.exit(7)
    if state.get('installed'): print('org.gnome.World.Secrets\tstable\t'+state.get('version','14.0'))
elif action=='info':
    assert '--show-origin' in args
    if not state.get('installed'): sys.exit(1)
    print(state.get('origin','atm-flathub'))
elif action=='remotes':
    if state.get('remote'): print('atm-flathub\t'+state.get('url','https://dl.flathub.org/repo/')+'\t'+state.get('options',''))
elif action=='remote-add':
    assert args[-2:]==['atm-flathub','https://dl.flathub.org/repo/flathub.flatpakrepo']
    pathlib.Path(os.environ['EXPECTED_FLATPAK']).mkdir(parents=True, exist_ok=True)
    state['remote']=True
elif action in ('install','update'):
    assert args[-1]=='org.gnome.World.Secrets//stable'
    if os.environ.get('NATIVE_CANCEL')!='1':
        state.update(installed=True,version='14.1' if action=='update' else '14.0')
elif action=='uninstall':
    assert args[-1]=='org.gnome.World.Secrets//stable'
    if os.environ.get('NATIVE_CANCEL')!='1': state['installed']=False
    if os.environ.get('POST_QUERY_FAIL')=='1': state['bad_query']=True
elif action=='run':
    assert '--branch=stable' in args and 'org.gnome.World.Secrets' in args
else: raise AssertionError(args)
path.write_text(json.dumps(state))
'''


class SecretsTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='atm-secrets-test-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.home = self.root / 'home'
        self.home.mkdir()
        self.apps = self.home / 'Apps with spaces'
        self.install = self.apps / 'secrets'
        self.flatpak = self.install / 'flatpak'
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        (self.bin / 'flatpak').write_text(MOCK)
        (self.bin / 'flatpak').chmod(0o755)
        (self.root / 'state.json').write_text('{}')
        self.env = {k: v for k, v in os.environ.items() if not k.startswith(('ATM_', 'FLATPAK_'))}
        self.env.update(HOME=str(self.home), ATM_HOME=str(REPO), ATM_LANG='en-us',
                        ATM_APPS_DIR=str(self.apps), ATM_MANIFEST_DIR=str(self.root/'manifests'),
                        ATM_PLUGIN_DIR=str(REPO/'plugins'), TEST_ROOT=str(self.root),
                        EXPECTED_FLATPAK=str(self.flatpak), FLATPAK_USER_DIR='/must-not-be-used',
                        XDG_DATA_HOME=str(self.home/'.local/share'), XDG_CONFIG_HOME=str(self.home/'.config'),
                        XDG_CACHE_HOME=str(self.home/'.cache'),
                        PATH=str(self.bin)+':'+os.environ['PATH'], SHLVL='1', TERM='dumb')

    def run_shell(self, code, reply='', **env):
        return subprocess.run(['bash','-c',PRELUDE+code],env=dict(self.env,**env),
                              input=reply,text=True,capture_output=True,timeout=10,cwd=REPO)

    def success(self, code, reply='', **env):
        result=self.run_shell(code,reply,**env)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        return result

    def calls(self):
        path=self.root/'calls'
        return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []

    def installed(self, **values):
        self.flatpak.mkdir(parents=True,exist_ok=True)
        (self.root/'state.json').write_text(json.dumps(dict(installed=True,remote=True,**values)))

    def test_install_update_launcher_and_scoped_manifest(self):
        self.success('atm_secrets_install', 'y\n')
        launcher=self.install/'bin/secrets'
        self.assertTrue(os.access(launcher,os.X_OK))
        self.assertEqual([x[0] for x in self.calls()].count('install'),1)
        self.assertIn('14.0',(self.root/'manifests/secrets.manifest').read_text())
        self.success('atm_secrets_install --update','y\n')
        self.assertIn('14.1',(self.root/'manifests/secrets.manifest').read_text())
        self.assertIn('Secrets 14.1 (ATM)',self.success('atm_secrets_status').stdout)
        self.assertEqual(self.success('atm_secrets_path_entries').stdout,str(launcher.parent)+'\n')
        result=subprocess.run([str(launcher),'file with spaces.kdbx'],env=self.env,capture_output=True,text=True)
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertEqual(self.calls()[-1][-1],'file with spaces.kdbx')

    def test_no_default_user_or_system_installation_is_adopted(self):
        default=self.home/'.local/share/flatpak/app/org.gnome.World.Secrets'
        default.mkdir(parents=True)
        result=self.success('atm_secrets_status')
        self.assertIn('not installed in the ATM',result.stdout)
        self.assertEqual(self.calls(),[])
        self.assertTrue(default.exists())

    def test_decline_eof_and_unknown_arguments_are_nonmutating(self):
        for reply in ('','\n','n\n'):
            self.success('atm_secrets_install',reply)
        self.assertEqual(self.calls(),[])
        self.assertFalse(self.install.exists())
        for cmd in ('atm_secrets_install --version 14.0','atm_secrets_remove all','atm_secrets_install --update extra'):
            self.assertNotEqual(self.run_shell(cmd).returncode,0)
        self.assertFalse(self.install.exists())

    def test_dry_runs_do_not_probe_or_write(self):
        for cmd in ('atm_secrets_install','atm_secrets_install --update','atm_secrets_remove','atm_secrets_open','atm_secrets_status'):
            self.success(cmd,ATM_DRY_RUN='1',FAIL='list')
        self.assertEqual(self.calls(),[])
        self.assertFalse(self.install.exists())
        self.assertFalse((self.root/'manifests').exists())

    def test_core_cli_routes_are_dry_run_safe_and_full_setup_is_disabled(self):
        for args in (['install','secrets'],['install','secrets','--update'],
                     ['remove','secrets','stable'],['uninstall','secrets']):
            result=subprocess.run(['bash',str(REPO/'bin/atm'),'--dry-run',*args],
                                  env=self.env,cwd=REPO,capture_output=True,text=True,timeout=10)
            self.assertEqual(result.returncode,0,result.stdout+result.stderr)
            self.assertIn('DRY-RUN:',result.stdout)
        self.assertEqual(self.calls(),[])
        self.assertEqual(list(self.home.iterdir()),[])
        self.success('''source "$ATM_HOME/lib/atm_plugin_loader.sh"
atm_plugin_arrays_init
atm_plugin_load_metadata "$ATM_HOME/plugins/secrets"
[[ "${ATM_PLUGIN_FULL_SETUP[secrets]}" == 0 ]]
''')

    def test_update_failure_preserves_manifest_and_missing_install_is_rejected(self):
        result=self.run_shell('atm_secrets_install --update','y\n')
        self.assertNotEqual(result.returncode,0)
        self.assertEqual(self.calls(),[])
        self.success('atm_secrets_install','y\n')
        before=(self.root/'manifests/secrets.manifest').read_bytes()
        result=self.run_shell('atm_secrets_install --update','y\n',FAIL='update')
        self.assertNotEqual(result.returncode,0)
        self.assertEqual((self.root/'manifests/secrets.manifest').read_bytes(),before)
        self.assertTrue((self.install/'bin/secrets').exists())

    def test_open_routes_only_to_private_installation(self):
        self.assertNotEqual(self.run_shell('atm_secrets_open').returncode,0)
        self.assertEqual(self.calls(),[])
        self.installed()
        self.success('atm_secrets_open')
        self.assertEqual(self.calls()[-1],['run','--user','--branch=stable','org.gnome.World.Secrets'])

    def test_missing_flatpak_and_non_linux_are_reported_without_sudo(self):
        result=self.run_shell('command() { if [[ "$*" == "-v flatpak" ]]; then return 1; else builtin command "$@"; fi; }; atm_secrets_install','y\n')
        self.assertNotEqual(result.returncode,0)
        self.assertIn('Flatpak is required',result.stderr)
        result=self.run_shell('uname() { printf Darwin; }; atm_secrets_install','y\n')
        self.assertNotEqual(result.returncode,0)
        self.assertIn('requires Linux',result.stderr)
        self.assertEqual(self.calls(),[])

    def test_install_failure_and_native_cancel_never_publish_launcher_or_manifest(self):
        for env in ({'FAIL':'install'},{'NATIVE_CANCEL':'1'}):
            result=self.run_shell('atm_secrets_install','y\n',**env)
            self.assertNotEqual(result.returncode,0)
            self.assertFalse((self.install/'bin/secrets').exists())
            self.assertFalse((self.root/'manifests').exists())

    def test_conflicting_launcher_and_linked_root_are_preserved(self):
        (self.install/'bin').mkdir(parents=True)
        launcher=self.install/'bin/secrets'
        launcher.write_text('unowned launcher')
        self.assertNotEqual(self.run_shell('atm_secrets_install','y\n').returncode,0)
        self.assertEqual(launcher.read_text(),'unowned launcher')
        shutil.rmtree(self.install)
        target=self.root/'outside'
        target.mkdir()
        self.install.symlink_to(target)
        self.assertNotEqual(self.run_shell('atm_secrets_install','y\n').returncode,0)
        self.assertEqual(list(target.iterdir()),[])
        self.assertEqual(self.calls(),[])

    def test_untrusted_remote_or_origin_blocks_update(self):
        for values in ({'url':'https://example.invalid/'},{'options':'no-gpg-verify'},
                       {'options':'disabled'},{'origin':'other'}):
            self.installed(**values)
            result=self.run_shell('atm_secrets_install --update','y\n')
            self.assertNotEqual(result.returncode,0)
        self.assertFalse(any(c[0]=='update' for c in self.calls()))

    def test_remove_preserves_vaults_app_data_runtimes_and_other_installations(self):
        self.success('atm_secrets_install','y\n')
        paths=[self.home/'.var/app/org.gnome.World.Secrets/config/settings',
               self.home/'Documents/vault.kdbx',self.flatpak/'runtime/keep',
               self.home/'.local/share/flatpak/app/org.gnome.World.Secrets/keep']
        for path in paths:
            path.parent.mkdir(parents=True,exist_ok=True)
            path.write_bytes(b'private fixture sentinel')
        self.success('atm_secrets_remove','y\n')
        for path in paths: self.assertEqual(path.read_bytes(),b'private fixture sentinel')
        self.assertFalse((self.install/'bin/secrets').exists())
        self.assertIn('ATM_INSTALLED=0',(self.root/'manifests/secrets.manifest').read_text())
        self.assertTrue(self.flatpak.exists())

    def test_failed_or_cancelled_removal_and_failed_query_do_not_claim_success(self):
        self.success('atm_secrets_install','y\n')
        before=(self.root/'manifests/secrets.manifest').read_bytes()
        for env in ({'FAIL':'uninstall'},{'NATIVE_CANCEL':'1'},{'POST_QUERY_FAIL':'1'}):
            self.installed()
            result=self.run_shell('atm_secrets_remove','y\n',**env)
            self.assertNotEqual(result.returncode,0)
            self.assertTrue((self.install/'bin/secrets').exists())
            self.assertEqual((self.root/'manifests/secrets.manifest').read_bytes(),before)
        self.assertIn('Cannot query',self.success('atm_secrets_status').stdout)

    def test_menu_routes_and_cancels(self):
        code='''atm_secrets_status() { :; }
atm_secrets_install() { printf 'INSTALL:%s\\n' "$*"; }
atm_secrets_open() { printf 'OPEN\\n'; }
atm_secrets_remove() { printf 'REMOVE\\n'; }
atm_secrets_menu
'''
        output=self.success(code,'1\n2\n3\n4\ninvalid\nq\n').stdout
        for token in ('INSTALL:', 'INSTALL:--update','OPEN','REMOVE'): self.assertIn(token,output)
        self.assertEqual(self.calls(),[])


if __name__=='__main__':
    unittest.main()
