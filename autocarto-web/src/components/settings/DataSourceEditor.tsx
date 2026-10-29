"use client";
import {useState} from 'react'
import {ActionButton} from './ActionBar'
import {Field, TextInput, SelectInput} from './FormField'
import type {DataCategory, DataSource, LocalDraft} from '@/features/acquisition/types'

export default function DataSourceEditor({draft, categories, editing, onSave, onCancel}: {draft: LocalDraft; categories: DataCategory[]; editing: boolean; onSave: (source: DataSource) => Promise<void>; onCancel: () => void}) {
 const [root, setRoot] = useState(draft.source.root || '')
 const [name, setName] = useState(draft.source.name)
 const [license, setLicense] = useState(draft.source.license || '')
 const [handbook,setHandbook]=useState(draft.source.handbook||{})
 const [rows, setRows] = useState(draft.entries.map(row => ({...row, selected: row.exists})))
 const [confirmed, setConfirmed] = useState(false)
 const [busy,setBusy] = useState(false)
 const [error,setError] = useState('')
 const change = (index: number, update: Partial<typeof rows[number]>) => {setRows(current => current.map((row,i) => i === index ? {...row,...update} : row)); setConfirmed(false)}
 const save = async () => {
  const selected = rows.filter(row => row.selected)
  if (!name.trim() || !selected.length || selected.some(row => !row.category)) {setError('请填写名称、选择数据，并为每条数据确认类别。'); return}
  if (!confirmed) {setError('请确认类别和字段对应后保存。');return}
  setBusy(true);setError('')
  try {await onSave({...draft.source, name: name.trim(), root: root.trim(), license: license.trim(), handbook, provides: selected.map(row => ({category: row.category, path: row.path, ...(row.layer?.trim() ? {layer:row.layer.trim()} : {}), fields: row.fields, params: row.params}))})} catch (e) {setError(e instanceof Error ? e.message : '保存失败')} finally {setBusy(false)}
 }
 return <div className="space-y-3 rounded border border-blue-300 p-3" aria-label="本机数据源草稿">
  <h4 className="text-sm font-medium">{editing ? '编辑本机数据源' : '确认扫描结果'}</h4>
  <div className="grid gap-3 sm:grid-cols-2"><Field label="数据源名称"><TextInput value={name} onChange={e=>setName(e.target.value)} disabled={busy}/></Field><Field label="许可说明（可选）"><TextInput value={license} onChange={e=>setLicense(e.target.value)} disabled={busy}/></Field></div>
  <details className="text-xs"><summary className="cursor-pointer">来源使用说明（可选）</summary><div className="mt-2 grid gap-2 sm:grid-cols-2">{([['good_for','适用任务'],['not_for','不适用任务'],['coverage','覆盖范围'],['scale','适用比例尺'],['fields','字段说明'],['cautions','注意事项']] as const).map(([key,label])=><Field key={key} label={label}><TextInput value={handbook[key]||''} onChange={e=>setHandbook({...handbook,[key]:e.target.value})}/></Field>)}</div></details>
  <Field label="数据根目录"><TextInput value={root} disabled={busy} onChange={e=>{setRoot(e.target.value);setConfirmed(false)}}/></Field>
  <p className="text-xs text-zinc-500">扫描结果仅作为建议。请确认每条数据的类别，以及标准字段在实际文件中的名称；留空表示采用默认字段规则。</p>
  {draft.messages.map((message,i)=><p className="text-xs" key={i}>{message}</p>)}
  <div className="max-h-[55vh] space-y-3 overflow-auto">{rows.map((row,index)=>{
   const category=categories.find(c=>c.id===row.category)
   return <fieldset key={index} className="space-y-2 rounded border border-zinc-200 p-2 dark:border-zinc-700" disabled={busy}>
    <label className="flex items-center gap-2 text-xs"><input type="checkbox" checked={row.selected} disabled={!row.exists} onChange={e=>change(index,{selected:e.target.checked})}/>{row.name}{!row.exists && '（文件不存在）'}</label>
    {editing ? <Field label={`相对路径：${row.name}`}><TextInput value={row.path} onChange={e=>change(index,{path:e.target.value})}/></Field> : <p className="break-all text-xs text-zinc-500">{row.path}</p>}
    {(row.layer || /\.gpkg$/i.test(row.path)) && <Field label={`图层名称（可选）：${row.name}`}><TextInput value={row.layer || ''} onChange={e=>change(index,{layer:e.target.value})} placeholder="多图层文件中要使用的图层"/></Field>}
    <Field label={`数据类别：${row.name}`}><SelectInput value={row.category} onChange={e=>change(index,{category:e.target.value,fields:{},params:{}})}><option value="">请选择类别</option>{categories.map(c=><option key={c.id} value={c.id}>{c.name}</option>)}</SelectInput></Field>
    {row.selected && category && <div className="grid gap-2 sm:grid-cols-2">{Object.entries(category.params || {}).map(([key,param])=><Field key={key} label={`${row.name} · ${key}${param.required?'（必填）':''}`}>{param.values?.length ? <SelectInput value={row.params?.[key]||''} onChange={e=>change(index,{params:{...row.params,[key]:e.target.value}})}><option value="">请选择</option>{param.values.map(value=><option key={value} value={value}>{value}</option>)}</SelectInput> : <TextInput value={row.params?.[key]||''} onChange={e=>change(index,{params:{...row.params,[key]:e.target.value}})}/>}</Field>)}{Object.entries(category.fields || {}).map(([key,field])=><Field key={key} label={`${row.name} · ${field.meaning}（${key}）`}><MappingInput key={`${row.category}:${key}`} values={row.fields[key]||[]} onChange={value=>{const fields={...row.fields};if(value.length)fields[key]=value;else delete fields[key];change(index,{fields})}}/></Field>)}</div>}
   </fieldset>
  })}</div>
  <label className="flex items-center gap-2 text-xs"><input type="checkbox" checked={confirmed} disabled={busy} onChange={e=>setConfirmed(e.target.checked)}/>我已确认所选数据的类别和字段对应</label>
  {error && <p role="alert" className="text-xs text-red-600">{error}</p>}
  <div className="flex gap-2"><ActionButton variant="primary" onClick={()=>void save()} disabled={busy || !confirmed}>{busy?'保存中…':'保存数据源'}</ActionButton><ActionButton onClick={onCancel} disabled={busy}>取消</ActionButton></div>
 </div>
}

function MappingInput({values,onChange}:{values:string[];onChange:(value:string[])=>void}){
 const [text,setText]=useState(values.join(', '))
 return <TextInput value={text} placeholder="文件中的字段名，多个候选用逗号分隔" onChange={e=>{setText(e.target.value);onChange(e.target.value.split(/[,，]/).map(v=>v.trim()).filter(Boolean))}}/>
}
