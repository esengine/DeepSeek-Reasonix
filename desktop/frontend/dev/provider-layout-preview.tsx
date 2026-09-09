// Isolated UI fixture. No native configuration or credentials are read or written.
import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { LocaleProvider } from '../src/lib/i18n';
import { ProviderConnections } from '../src/components/ProviderConnections';
import { providerAccessGroups, ProviderEditor, ProvidersSection, ModelsSection } from '../src/components/SettingsPanel';
import { SettingsNavigation, SETTINGS_NAV_TABS } from '../src/components/SettingsNavigation';
import { useT } from '../src/lib/i18n';
import { ConnectionTitle } from '../src/components/ConnectionTitle';
import { baseSettings } from '../src/test-support/settingsTestFixtures';
import type { ProviderView } from '../src/lib/types';
import '../src/styles.css';
import '../src/components/SettingsPanel.css';
const models = ['deepseek-v4-flash', 'deepseek-v4-pro', 'deepseek-v4-flash-vision-exp'];
const sample = {name:'deepseek', builtIn:true, added:true, kind:'anthropic', baseUrl:'https://api.deepseek.com/anthropic', models, default:models[0], apiKeyEnv:'DEEPSEEK_API_KEY', keySet:true, visionModels:[], modelsUrl:'', balanceUrl:'', supportedEfforts:[], contextWindow:1000000, webSearch:true, modelCapabilities: models.map((model,i)=>({model,state:i===2 ? 'supported' : 'unsupported'}))} as ProviderView;
(window as any).go = {main:{App:{FetchProviderModelCatalog:async()=>sample.modelCapabilities}}};
function OnboardingPreview() {
 const [providers,setProviders]=useState<ProviderView[]>(location.search.includes('existing') ? [{...sample,keySet:false}] : []);
 const [done,setDone]=useState(false);
 (window as any).go.main.App.AddProviderConnection=async (_id:string,_source:string,key:string)=>setProviders([{...sample,keySet:Boolean(key)}]);
 (window as any).go.main.App.SetDefaultModel=async ()=>{};

 const settings={...baseSettings(),providers,providerPresets:[],providerKinds:['anthropic','openai','responses']};
 return <main style={{padding:24,maxWidth:1300,margin:'auto'}}><button className="btn" onClick={()=>setDone(true)}>返回工作区</button>{done ? <p>已返回工作区（预览）</p> : <div className="model-access-page"><ProvidersSection s={settings} onboarding busy={false} apply={async fn=>{await fn();return true;}} onOnboardingComplete={()=>setDone(true)}/></div>}</main>;
}
function ScrollPreview() {
 const [provider,setProvider]=useState(sample);
 const [page,setPage]=useState<any>("providers"); const t=useT();
 const groups=providerAccessGroups(Array.from({length:12},(_,i)=>({...provider,name:i ? `connection-${i}` : provider.name,displayName:`DeepSeek ${i+1}`})), ((key:string)=>key) as any);
 return <div className="settings-center" style={{height:"100dvh"}}><SettingsNavigation items={SETTINGS_NAV_TABS.map(id=>({id,label:id==="models"?"模型偏好":id==="providers"?"模型服务":id==="model-stats"?"用量统计":t(`settings.tab.${id}` as any),meta:""}))} activeTab={page} onSelect={setPage}/><main className="settings-center__content" style={{height:'100dvh',boxSizing:'border-box'}}><div className="settings-page-content"><div className="settings-page settings-page--models">
 <header className="settings-page__header"><h2>{page==="providers"?"模型服务":page==="models"?"模型偏好":"用量统计"}</h2></header>{page!=="providers" && <p>导航预览：此页面的原有内容在应用中保留。</p>}<div className="model-access-page" hidden={page!=="providers"}>
 <section className="settings-section"><div className="settings-section__head">供应商接入</div><div className="settings-section__body"><div className="provider-access-grid">
 <ProviderConnections groups={groups} presets={[]} revealedProvider={null} hidden={false} busy={false} onAdd={()=>{}} renderDetail={group=><article className="provider-access-card provider-access-card--detail"><div className="provider-access-card__head"><ConnectionTitle label={group.label} busy={false} onSave={async()=>true}/></div><div className="provider-access-card__desc">Anthropic Messages</div><ProviderEditor hideConnectionName initial={provider} kinds={['anthropic','openai','responses']} busy={false} onCancel={()=>{}} onSave={async p=>setProvider(p)} /></article>}/>
 </div></div></section></div></div></div></main></div>;
}
function PreferencesPreview() {
 const t=useT();
 const [settings,setSettings]=useState({...baseSettings(),defaultModel:'deepseek/deepseek-v4-flash',visionModel:'auto',webSearchModel:'auto',webSearchModels:['deepseek/deepseek-v4-flash','deepseek/deepseek-v4-pro'],providers:[{...sample,displayName:'DeepSeek 官方1'}]});
 const setters: Record<string,string> = {SetDefaultModel:'defaultModel',SetPlannerModel:'plannerModel',SetVisionModel:'visionModel',SetWebSearchModel:'webSearchModel',SetSubagentModel:'subagentModel',SetSubagentEffort:'subagentEffort'};
 Object.entries(setters).forEach(([method,key])=>{(window as any).go.main.App[method]=async(value:string)=>setSettings(s=>({...s,[key]:value}));});
 const agentSetters: Record<string,string>={SetReasoningLanguage:'reasoningLanguage',SetCompactRatio:'compactRatio',SetMaxSubagentDepth:'maxSubagentDepth',SetMaxSubagentConcurrency:'maxSubagentConcurrency',SetMaxParallelWriters:'maxParallelWriters'};
 Object.entries(agentSetters).forEach(([method,key])=>{(window as any).go.main.App[method]=async(value:unknown)=>setSettings(s=>({...s,agent:{...s.agent,[key]:value}}));});
 return <div className="settings-center" style={{height:'100vh'}}><SettingsNavigation items={SETTINGS_NAV_TABS.map(id=>({id,label:id==="models"?"模型偏好":id==="model-stats"?"用量统计":t(`settings.tab.${id}` as any),meta:""}))} activeTab="models" onSelect={()=>{}}/><main className="settings-center__content"><div className="settings-page-content"><div className="settings-page settings-page--models"><ModelsSection s={settings} busy={false} subtab="usage" apply={async fn=>{await fn();return true;}} backgroundApply={async()=>{}}/></div></div></main></div>;
}
function Preview() {
 const [provider,setProvider]=useState(sample), [label,setLabel]=useState('DeepSeek 官方'), [revision,setRevision]=useState(0);
 return <main style={{padding:'28px 40px',maxWidth:1360,margin:'auto',background:'var(--bg-elev)'}}>
 <h2 style={{fontSize:24,margin:'0 0 16px'}}><ConnectionTitle label={label} busy={false} onSave={async(value)=>{setLabel(value);return true;}}/></h2>
 <ProviderEditor key={revision} hideConnectionName initial={provider} kinds={['anthropic','openai','responses']} busy={false} onCancel={()=>setRevision(r=>r+1)} onSave={async(p)=>setProvider(p)} onSaveKey={async()=>setProvider(p=>({...p,keySet:true}))} onClearKey={async()=>setProvider(p=>({...p,keySet:false}))}/>
 </main>;
}
createRoot(document.getElementById('root')!).render(location.search.includes('narrow') ? <iframe title="Narrow provider editor" src="./provider-layout-preview.html" style={{width:390,height:850,border:0}}/> : <LocaleProvider>{location.search.includes("preferences") ? <PreferencesPreview/> : location.search.includes("onboarding") ? <OnboardingPreview/> : location.search.includes("scroll") ? <ScrollPreview/> : <Preview/>}</LocaleProvider>);
