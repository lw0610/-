"use client";
import {useCallback,useEffect,useState} from 'react'
import Link from 'next/link'
import {acquisitionApi} from '@/features/acquisition/api'
import type {AcquisitionConfig, DataMemoryEntry, DataSource, DataStore, LocalDraft, SourceCatalog} from '@/features/acquisition/types'
import {isDesktopRuntime,pickDirectoryPath,pickFilePath} from '@/lib/config-form-helpers'
import PathDialog from '@/features/guides/PathDialog'
import {ActionButton} from './ActionBar'
import SettingSection from './SettingSection'
import {Field,TextInput,NumberInput,SelectInput} from './FormField'
import DataSourceEditor from './DataSourceEditor'
const size = (bytes: number) => bytes >= 1024**3 ? `${(bytes/1024**3).toFixed(2)} GB` : `${(bytes/1024**2).toFixed(1)} MB`
const kindNames: Record<string,string> = {local_folder:'本机文件夹',offline_asset:'内置离线数据',http_file:'文件下载',overpass:'OpenStreetMap 查询',gee:'Earth Engine',tianditu:'天地图',portal:'数据门户',web_search:'联网搜索',ogc_wfs:'WFS 服务',arcgis_rest:'ArcGIS 服务'}
export default function DataAcquisitionWorkspace(){
 const [catalog,setCatalog]=useState<SourceCatalog>({sources:[],categories:[],diagnostics:[]})
 const [store,setStore]=useState<DataStore>({items:[],used_bytes:0,max_bytes:0,directory:''})
 const [config,setConfig]=useState<AcquisitionConfig|null>(null)
 const [memory,setMemory]=useState<DataMemoryEntry[]>([])
 const [models,setModels]=useState<{name:string;model:string}[]>([])
 const [confirm,setConfirm]=useState<'when_needed'|'always'|'never'>('when_needed')
 const [savedConfirm,setSavedConfirm]=useState('when_needed')
 const [savedConfig,setSavedConfig]=useState('')
 const [busy,setBusy]=useState(false),[loaded,setLoaded]=useState(false),[error,setError]=useState(''),[status,setStatus]=useState('')
 const [path,setPath]=useState(''),[draft,setDraft]=useState<LocalDraft|null>(null),[editing,setEditing]=useState(false)
 const [deletion,setDeletion]=useState<{kind:'source'|'store'|'expired';id:string;label:string;references?:string[]}|null>(null)
 const refreshData=useCallback(async()=>{const [sources,records]=await Promise.all([acquisitionApi.sources(),acquisitionApi.store()]);setCatalog(sources);setStore(records)},[])
 const refresh=useCallback(async()=>{const [sources,records,settings,planSettings,remembered,providers]=await Promise.all([acquisitionApi.sources(),acquisitionApi.store(),acquisitionApi.config(),acquisitionApi.planConfig(),acquisitionApi.memory(),acquisitionApi.models()]);setMemory(remembered.entries);setModels(providers.providers.map(p=>({name:p.name,model:p.model})));setConfirm(planSettings.confirm);setSavedConfirm(planSettings.confirm);setCatalog(sources);setStore(records);setConfig(settings);setSavedConfig(JSON.stringify(settings));setLoaded(true)},[])
 const act=useCallback(async(action:()=>Promise<void>)=>{setBusy(true);setError('');setStatus('');try{await action()}catch(e){setError(e instanceof Error?e.message:'操作失败')}finally{setBusy(false)}},[])
 useEffect(()=>{void act(refresh)},[act,refresh])
 const scan=()=>act(async()=>{const next=await acquisitionApi.scan(path.trim());setDraft(next);setEditing(false)})
 const browse=(directory:boolean)=>act(async()=>{const value=await(directory?pickDirectoryPath():pickFilePath());if(value)setPath(value)})
 const edit=(source:DataSource)=>{setEditing(true);setDraft({source,entries:source.provides.map(p=>({name:p.path||p.category,path:p.path||'',layer:p.layer,kind:'',exists:true,category:p.category,fields:p.fields||{},params:p.params})),messages:[]})}
 const saveConfig=()=>act(async()=>{if(!config)return;if(config.max_download_mb<1||config.store_max_gb<=0||config.concurrency<1||config.per_host<1||config.per_host>config.concurrency)throw new Error('请填写有效的下载大小、容量和并发限制。');await acquisitionApi.saveConfig(config);await acquisitionApi.savePlanConfig(confirm);setSavedConfirm(confirm);setSavedConfig(JSON.stringify(config));await refreshData();setStatus('数据获取设置已保存')})
 return <div className="space-y-3">
  <div className="flex items-center justify-between"><h2 className="text-sm font-semibold">数据获取</h2><ActionButton disabled={busy} onClick={()=>void act(refresh)}>{busy?'处理中…':'刷新'}</ActionButton></div>
  {error && <p role="alert" className="rounded border border-red-200 p-2 text-xs text-red-600">{error}</p>}{status && <p role="status" className="text-xs text-emerald-700">{status}</p>}
  <SettingSection title="数据来源" description="声明来源的类别、许可和本机配置状态。设置页不会联网检查、下载数据或调用模型。">
   <p className="mb-2 text-xs text-zinc-500">内置来源由开发者维护。本机数据源保存文件位置与字段对应，原文件留在你选择的目录。服务开关与授权请到<Link href="/settings/system" className="text-blue-600 underline">扩展服务设置</Link>。</p>
   <div className="space-y-2">{catalog.sources.map(source=><div key={source.id} className="rounded border border-zinc-200 p-2 dark:border-zinc-700">
    <div className="flex flex-wrap items-center justify-between gap-2"><span className="text-xs font-medium">{source.name} · {source.builtin?'内置 · 只读':'本机'}</span>{!source.builtin && <div className="flex gap-2"><ActionButton disabled={busy||!!draft} onClick={()=>edit(source)}>编辑</ActionButton><ActionButton disabled={busy} onClick={()=>setDeletion({kind:'source',id:source.id,label:source.name})}>删除</ActionButton></div>}</div>
    <p className="mt-1 text-xs text-zinc-500">{kindNames[source.kind]||source.kind} · {source.provides.map(p=>catalog.categories.find(c=>c.id===p.category)?.name||p.category).filter((value,index,all)=>all.indexOf(value)===index).join('、')}</p>
    <p className="text-xs text-zinc-500">来源标签：{{unknown:'未核实',open:'开放数据',cn_official:'国内官方',cn_reference:'国内参考'}[source.compliance||'unknown']||source.compliance} · 许可：{source.license||'未提供'}</p>
    {source.root && <p className="break-all text-xs text-zinc-500">{source.root}</p>}
    <p className={`mt-1 text-xs ${source.available?'text-emerald-700':'text-zinc-600 dark:text-zinc-400'}`}>{source.available?'本机配置可用；实际访问状态将在使用时检查':(source.reasons||[]).join('；')}</p>
    {source.handbook&&<details className="mt-2 text-xs"><summary className="cursor-pointer text-blue-600">来源使用说明</summary>{Object.entries({适用:source.handbook.good_for,不适用:source.handbook.not_for,覆盖范围:source.handbook.coverage,适用比例尺:source.handbook.scale,字段:source.handbook.fields,注意事项:source.handbook.cautions}).map(([label,value])=>value?<p key={label} className="mt-1">{label}：{value}</p>:null)}{source.handbook.examples?.map((example,i)=><p key={i}>示例：{example}</p>)}</details>}
    {(source.notes||[]).map((note,i)=><p key={i} className="text-xs text-zinc-500">{note}</p>)}
   </div>)}</div>
   {catalog.diagnostics.map((item,i)=><p className="mt-2 text-xs" key={i}>{item.source_id ? `${item.source_id}：` : ''}{item.message}</p>)}
   {loaded && !catalog.sources.length && <p className="text-xs text-zinc-500">暂无可用的数据来源。</p>}
   <div className="mt-3 space-y-2"><Field label="添加本机数据（文件夹或 CSV/XLSX 数据清单）"><TextInput value={path} onChange={e=>setPath(e.target.value)} disabled={busy||!!draft} placeholder="输入完整路径，或选择文件夹 / 清单"/></Field><div className="flex gap-2">{isDesktopRuntime() && <><ActionButton disabled={busy||!!draft} onClick={()=>void browse(true)}>选择文件夹</ActionButton><ActionButton disabled={busy||!!draft} onClick={()=>void browse(false)}>选择清单</ActionButton></>}<ActionButton disabled={busy||!!draft||!path.trim()} onClick={()=>void scan()}>扫描并预览</ActionButton></div></div>
   {draft && <div className="mt-3"><DataSourceEditor key={draft.source.id} draft={draft} categories={catalog.categories} editing={editing} onCancel={()=>setDraft(null)} onSave={async source=>{setBusy(true);try{await acquisitionApi.saveSource(source,editing);await refreshData();setDraft(null);setPath('');setStatus('本机数据源已保存')}finally{setBusy(false)}}}/></div>}
  </SettingSection>
  <SettingSection title="数据仓" description="缓存自动获取的结果；删除不影响原始来源。清理过期数据会保留已有运行引用的条目。" rightSlot={<ActionButton disabled={busy||!store.items.some(item=>item.expired)} onClick={()=>setDeletion({kind:'expired',id:'',label:'过期数据'})}>清理过期数据</ActionButton>}>
   <p className="mb-2 text-xs">已使用 {size(store.used_bytes)} / {size(store.max_bytes)}</p><p className="mb-2 break-all text-xs text-zinc-500">{store.directory}</p>
   {store.items.length===0 && <p className="text-xs text-zinc-500">暂无已获取的数据。</p>}
   <div className="space-y-2">{store.items.map(item=><div key={item.key} className="rounded border border-zinc-200 p-2 text-xs dark:border-zinc-700"><div className="flex items-center justify-between gap-2"><span>{catalog.categories.find(c=>c.id===item.spec.category)?.name||item.spec.category} · {catalog.sources.find(s=>s.id===item.spec.source_id)?.name||item.spec.source_id}</span><ActionButton disabled={busy} onClick={()=>setDeletion({kind:'store',id:item.key,label:item.spec.category,references:item.referencing_runs})}>删除数据</ActionButton></div><p className="text-zinc-500">{size(item.bytes)} · {item.expired?'已过期':item.valid?'有效':'文件不完整'} · 获取于 {new Date(item.created_at).toLocaleString()}</p><p className="text-zinc-500">有效期：{item.expires_at?new Date(item.expires_at).toLocaleString():'长期'} · 范围：{item.spec.bbox?.join(', ')||'来源全范围'}</p><p className="break-all text-zinc-500">引用运行：{item.referencing_runs.join('、')||'无'}</p></div>)}</div>
  </SettingSection>
  {config && <SettingSection title="获取限制" description="限制单次下载和数据仓容量，控制来源失败后的回退行为。">
   <div className="grid gap-3 sm:grid-cols-2"><Field label="最大下载大小（MB）"><NumberInput min={1} max={102400} value={config.max_download_mb} onChange={e=>setConfig({...config,max_download_mb:Number(e.target.value)})}/></Field><Field label="数据仓容量（GB）"><NumberInput min={0.1} max={10240} step={0.1} value={config.store_max_gb} onChange={e=>setConfig({...config,store_max_gb:Number(e.target.value)})}/></Field><Field label="总并发数"><NumberInput min={1} max={16} value={config.concurrency} onChange={e=>setConfig({...config,concurrency:Number(e.target.value)})}/></Field><Field label="同一主机并发数"><NumberInput min={1} max={config.concurrency} value={config.per_host} onChange={e=>setConfig({...config,per_host:Number(e.target.value)})}/></Field></div>
   <div className="mt-3 grid gap-3 sm:grid-cols-2">
    <Field label="联网获取数据"><SelectInput value={config.network_policy||'direct'} onChange={e=>setConfig({...config,network_policy:e.target.value as AcquisitionConfig['network_policy']})}><option value="direct">直接获取</option><option value="confirm">开始前确认计划</option><option value="deny">禁止联网获取</option></SelectInput></Field>
    <Field label="没有可用来源时"><SelectInput value={config.fallback_policy||'ask'} onChange={e=>setConfig({...config,fallback_policy:e.target.value as AcquisitionConfig['fallback_policy']})}><option value="ask">询问是否尝试其他办法</option><option value="allow">允许尝试其他办法</option><option value="deny">停止获取</option></SelectInput></Field>
    <Field label="数据获取智能体模型"><SelectInput value={config.agent_model||''} onChange={e=>setConfig({...config,agent_model:e.target.value})}><option value="">跟随主模型</option>{models.map(p=><option key={p.name} value={p.name}>{p.name} · {p.model}</option>)}</SelectInput></Field>
    <Field label="计划确认"><SelectInput value={confirm} onChange={e=>setConfirm(e.target.value as typeof confirm)}><option value="when_needed">需要时确认</option><option value="always">每次确认</option><option value="never">自动开始</option></SelectInput></Field>
   </div>
   <label className="mt-3 flex items-center gap-2 text-xs"><input type="checkbox" checked={config.exclusive_agent!==false} onChange={e=>setConfig({...config,exclusive_agent:e.target.checked})}/>只允许数据获取智能体和计划执行器取数</label>
   <label className="mt-2 flex items-center gap-2 text-xs"><input type="checkbox" checked={config.web_search_enabled} onChange={e=>setConfig({...config,web_search_enabled:e.target.checked})}/>允许联网搜索数据</label><p className="mt-1 text-xs text-zinc-500">联网搜索用于现有来源不可用时的补充查找，是否继续遵循上方策略。需要登录的来源会提示处理。</p>
   <ActionButton className="mt-3" variant="primary" disabled={busy||(JSON.stringify(config)===savedConfig&&confirm===savedConfirm)} onClick={()=>void saveConfig()}>保存获取设置</ActionButton>
  </SettingSection>}
  <SettingSection title="数据记忆" description="记住已成功使用的数据位置。清除记录不会删除数据文件。">
   {!memory.length&&<p className="text-xs text-zinc-500">暂无数据记忆。</p>}
   {memory.map(entry=><div key={[entry.guide,entry.name,entry.category,entry.extent].join('|')} className="mb-2 flex items-center justify-between gap-2 rounded border p-2 text-xs"><div><p>{entry.name} · {entry.guide||entry.category||'通用'}</p><p className="break-all text-zinc-500">{entry.path||entry.source_id} {entry.extent&&`· ${entry.extent}`}</p></div><ActionButton disabled={busy} onClick={()=>void act(async()=>{await acquisitionApi.forget(entry);setMemory((await acquisitionApi.memory()).entries);setStatus('已清除数据记忆，原文件保留')})}>清除记忆</ActionButton></div>)}
  </SettingSection>
  {deletion && <PathDialog title={deletion.kind==='source'?'删除本机数据源':deletion.kind==='expired'?'清理过期数据':'删除数据仓条目'} fields={[]} description={<div className="space-y-2 text-sm"><p>{deletion.kind==='source'?`删除“${deletion.label}”的来源配置，原始数据文件保留。`:deletion.kind==='expired'?'将删除过期且没有运行引用的缓存数据。':`将删除“${deletion.label}”的已获取副本；需要时须重新获取。`}</p>{!!deletion.references?.length && <p>以下运行曾引用这份数据：{deletion.references.join('、')}。运行记录会保留，但数据仓中的文件将被删除。</p>}</div>} submitLabel="确认删除" onClose={()=>setDeletion(null)} onSubmit={async()=>{if(deletion.kind==='source')await acquisitionApi.deleteSource(deletion.id);else if(deletion.kind==='store')await acquisitionApi.deleteStored(deletion.id);else await acquisitionApi.cleanExpired();await refreshData();setDeletion(null);setStatus('删除完成')}}/>}
 </div>
}
