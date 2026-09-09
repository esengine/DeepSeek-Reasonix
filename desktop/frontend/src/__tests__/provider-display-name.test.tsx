import {JSDOM} from 'jsdom';
import React,{act} from 'react';

import assert from 'node:assert/strict';

import {LocaleProvider} from '../lib/i18n';
import type {ProviderView} from '../lib/types';
const dom=new JSDOM('<div id="root"></div>',{url:'http://localhost',pretendToBeVisual:true});
Object.assign(globalThis,{window:dom.window,document:dom.window.document,localStorage:dom.window.localStorage,IS_REACT_ACT_ENVIRONMENT:true});
const {createRoot}=await import('react-dom/client');
const {ProviderEditor,providerAccessGroups}=await import('../components/SettingsPanel');
const provider={name:'stable-id',displayName:'工作账号',kind:'openai',baseUrl:'https://example.com/v1',models:['model'],default:'model',apiKeyEnv:'TEST_KEY',keySet:true,added:true,builtIn:false,visionModels:[],supportedEfforts:[],modelsUrl:'',balanceUrl:'',contextWindow:100000} as ProviderView;
let saved:ProviderView|undefined;
const root=createRoot(document.getElementById('root')!);
await act(async()=>root.render(<LocaleProvider><ProviderEditor initial={provider} kinds={['openai']} busy={false} onCancel={()=>{}} onSave={p=>{saved=p;}} /></LocaleProvider>));
const name=document.querySelector('.provider-name-input') as HTMLInputElement;
assert.equal(name.disabled,false);assert.equal(name.value,'工作账号');
await act(async()=>{
Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype,'value')!.set!.call(name,'个人账号');
name.dispatchEvent(new window.Event('input',{bubbles:true}));
});
const save=document.querySelector('.provider-editor-footer .btn--primary') as HTMLButtonElement;
await act(async()=>save.click());
assert.equal(saved?.name,'stable-id');assert.equal(saved?.displayName,'个人账号');
assert.equal(providerAccessGroups([provider],((key:string)=>key) as any)[0].label,'工作账号');
await act(async()=>root.render(<LocaleProvider><ProviderEditor key="builtin" initial={{...provider,builtIn:true}} kinds={['openai','anthropic','responses']} busy={false} onCancel={()=>{}} onSave={p=>{saved=p;}} /></LocaleProvider>));
assert.equal((document.querySelector('button[aria-label="API format"]') as HTMLButtonElement).disabled,false);
assert.ok(document.querySelector('.provider-model-toolbar'));
assert.equal(document.querySelector('.provider-editor--key-only'),null);
assert.ok(document.querySelector('.provider-name-input'));
await act(async()=>root.unmount());
console.log('PASS: editable connection label saves independently of stable identity and drives list title');
