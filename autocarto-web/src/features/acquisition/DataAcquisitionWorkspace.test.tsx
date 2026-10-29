import {beforeEach, expect, it, vi} from 'vitest'
import {fireEvent, render, screen, waitFor, within} from '@testing-library/react'
import DataAcquisitionWorkspace from '@/components/settings/DataAcquisitionWorkspace'
const mocks=vi.hoisted(()=>({planConfig:vi.fn(),savePlanConfig:vi.fn(),memory:vi.fn(),forget:vi.fn(),sources:vi.fn(),store:vi.fn(),config:vi.fn(),scan:vi.fn(),saveSource:vi.fn(),deleteSource:vi.fn(),deleteStored:vi.fn(),cleanExpired:vi.fn(),saveConfig:vi.fn()}))
vi.mock('@/features/acquisition/api',()=>({acquisitionApi:mocks}))
vi.mock('@/lib/config-form-helpers',()=>({isDesktopRuntime:()=>false,pickDirectoryPath:vi.fn(),pickFilePath:vi.fn()}))
const local={api_version:'data_source/v1',id:'user.fixture',name:'本机铁路',kind:'local_folder',root:'/fixture',provides:[{category:'transport.railway',path:'rail.geojson',fields:{name:['NAME']}}],builtin:false,available:true}
const categories=[{id:'transport.railway',name:'铁路线',fields:{name:{meaning:'名称'}}}]
beforeEach(()=>{
 vi.resetAllMocks()
 mocks.planConfig.mockResolvedValue({confirm:'when_needed'});mocks.savePlanConfig.mockResolvedValue({});mocks.memory.mockResolvedValue({entries:[]})
 mocks.sources.mockResolvedValue({sources:[{...local,id:'source.example',name:'内置来源',kind:'overpass',builtin:true,available:false,reasons:['需要启用 OpenStreetMap']},local],categories,diagnostics:[]})
 mocks.store.mockResolvedValue({items:[{key:'cache-fixture',spec:{category:'transport.railway',source_id:'user.fixture',bbox:[100,20,101,21]},bytes:1024,valid:true,expired:true,created_at:'2026-10-01T00:00:00Z',expires_at:'2026-10-02T00:00:00Z',referencing_runs:['run-example']}],used_bytes:1024,max_bytes:1024**3,directory:'/runtime/data/acquired'})
 mocks.config.mockResolvedValue({max_download_mb:2048,store_max_gb:20,concurrency:2,per_host:1,auto_fallback:true,web_search_enabled:false,overpass_endpoint:''})
 mocks.scan.mockResolvedValue({source:{...local,id:'user.draft'},entries:[{name:'铁路',path:'rail.geojson',kind:'vector',exists:true,category:'transport.railway',fields:{name:['NAME']}}],messages:[]})
 mocks.saveSource.mockResolvedValue({});mocks.deleteSource.mockResolvedValue({});mocks.deleteStored.mockResolvedValue({});mocks.saveConfig.mockResolvedValue({});mocks.cleanExpired.mockResolvedValue({deleted:[],retained_referenced:['cache-fixture']})
})
it('shows precise source readiness and keeps builtin entries read only', async()=>{
 render(<DataAcquisitionWorkspace/>)
 expect(await screen.findByText('内置来源 · 内置 · 只读')).toBeInTheDocument()
 expect(screen.getByText('需要启用 OpenStreetMap')).toBeInTheDocument()
 expect(screen.queryByRole('button',{name:'编辑'})).not.toBeInTheDocument()
 expect(screen.queryByText('本机铁路')).not.toBeInTheDocument()
 expect(screen.getByRole('link',{name:'内置图层库'})).toHaveAttribute('href','/guides?tab=layers')
 expect(screen.getByRole('checkbox',{name:'允许联网搜索数据'})).not.toBeChecked()
 expect(screen.getByText(/是否继续遵循上方策略/)).toBeInTheDocument()
})
it('moves local registration out of settings and exposes reading capability',async()=>{
 mocks.config.mockResolvedValue({max_download_mb:2048,store_max_gb:20,concurrency:2,per_host:1,web_search_enabled:false,reading_status:{mode:'ocr',available:true,model:'text-model',engine:'tesseract',message:'本机中文 OCR 可用'}})
 render(<DataAcquisitionWorkspace/>);expect(await screen.findByText('本机 OCR：可用')).toBeVisible();expect(screen.queryByText('扫描并预览')).not.toBeInTheDocument();expect(screen.getByText('本机中文 OCR 可用')).toBeVisible();expect(screen.queryByRole('combobox',{name:'数据获取智能体模型'})).not.toBeInTheDocument();expect(screen.queryByRole('checkbox',{name:'只允许数据获取智能体和计划执行器取数'})).not.toBeInTheDocument()
})
it('shows affected runs before deleting cached data',async()=>{
 render(<DataAcquisitionWorkspace/>)
 fireEvent.click(await screen.findByRole('button',{name:'删除数据'}))
 const dialog=await screen.findByRole('dialog')
 expect(within(dialog).getByText(/以下运行曾引用这份数据：run-example/)).toBeInTheDocument()
 expect(mocks.deleteStored).not.toHaveBeenCalled()
 fireEvent.click(within(dialog).getByRole('button',{name:'确认删除'}))
 await waitFor(()=>expect(mocks.deleteStored).toHaveBeenCalledWith('cache-fixture'))
})
it('saves limits with web search opt in and keeps pending settings when data is edited',async()=>{
 render(<DataAcquisitionWorkspace/>)
 const limit=await screen.findByRole('spinbutton',{name:'最大下载大小（MB）'})
 fireEvent.change(limit,{target:{value:'300'}})
 fireEvent.click(screen.getByRole('checkbox',{name:'允许联网搜索数据'}))
 fireEvent.click(screen.getByRole('button',{name:'保存获取设置'}))
 await waitFor(()=>expect(mocks.saveConfig).toHaveBeenCalledWith(expect.objectContaining({max_download_mb:300,web_search_enabled:true})))
 expect(await screen.findByRole('status')).toHaveTextContent('数据获取设置已保存')
})

it('saves OCR paths without execution budget settings',async()=>{
 render(<DataAcquisitionWorkspace/>);await screen.findByText('本机 OCR 设置');expect(screen.queryByText('执行预算')).not.toBeInTheDocument();fireEvent.click(screen.getByText('本机 OCR 设置'));fireEvent.change(screen.getByRole('textbox',{name:'Tesseract 命令或路径'}),{target:{value:'/opt/bin/tesseract'}});fireEvent.click(screen.getByRole('button',{name:'保存获取设置'}));await waitFor(()=>expect(mocks.saveConfig).toHaveBeenCalledWith(expect.objectContaining({ocr_path:'/opt/bin/tesseract'})))
})

it('saves plan and acquisition policies together without exposing old flow policy',async()=>{
 render(<DataAcquisitionWorkspace/>);await screen.findByRole('combobox',{name:'计划确认'});
 expect(screen.queryByText('流程策略')).not.toBeInTheDocument();
 fireEvent.change(screen.getByRole('combobox',{name:'联网获取数据'}),{target:{value:'deny'}});
 fireEvent.change(screen.getByRole('combobox',{name:'计划确认'}),{target:{value:'always'}});
 fireEvent.click(screen.getByRole('button',{name:'保存获取设置'}));
 await waitFor(()=>expect(mocks.saveConfig).toHaveBeenCalledWith(expect.objectContaining({network_policy:'deny'})));
 expect(mocks.savePlanConfig).toHaveBeenCalledWith('always');
});
