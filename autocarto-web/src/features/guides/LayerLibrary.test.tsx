import {beforeEach,expect,it,vi} from 'vitest'
import {fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import LayerLibrary from './LayerLibrary'
import type {LayerItem} from './types'
const mocks=vi.hoisted(()=>({list:vi.fn(),scan:vi.fn(),import:vi.fn(),update:vi.fn(),remove:vi.fn(),regeneratePreview:vi.fn(),load:vi.fn()}))
vi.mock('./api',()=>({layerApi:mocks,loadLayerPreview:mocks.load}))
vi.mock('@/lib/config-form-helpers',()=>({isDesktopRuntime:()=>false}))
const layer=(scope:'system'|'user'='user'):LayerItem=>({api_version:'layer/v1',id:'river',name:'河流线',aliases:['水系'],pack:'中国底图',category:'river',geometry:'line',file:'/data/river.gpkg',crs:'EPSG:4326',declared_crs:'EPSG:4326',feature_count:20,scope,status:'ready',coverage:{bbox:[110,25,116,30],regions:['湖南']},source:{title:'示例来源',license:'开放许可'},compliance:'来源已确认'})
beforeEach(()=>{vi.clearAllMocks();mocks.list.mockResolvedValue({items:[layer()],categories:[{id:'river',name:'河流'}],migration_suggestions:[{path:'/old/China_map',pack:'旧底图',source:'legacy',message:'发现已登记的旧数据文件夹'}]});mocks.load.mockRejectedValue(new Error('此测试不绘制图片'));mocks.update.mockImplementation(async (_id,edit)=>({...layer(),...edit}));mocks.remove.mockResolvedValue({});mocks.import.mockResolvedValue({items:[layer()]})})
it('finds aliases, displays metadata and deletes only the registration after confirmation',async()=>{
 render(<LayerLibrary/>);fireEvent.change(screen.getByLabelText('搜索内置图层'),{target:{value:'水系'}});fireEvent.click(await screen.findByRole('button',{name:/河流线/}));expect(screen.getByText(/实际使用：EPSG:4326/)).toBeVisible();expect(screen.getByRole('img',{name:'图层覆盖范围示意'})).toBeVisible();fireEvent.click(screen.getByText('删除登记'));expect(mocks.remove).not.toHaveBeenCalled();expect(screen.getByText(/原始数据文件保留/)).toBeVisible();fireEvent.click(screen.getByText('确认删除登记'));await waitFor(()=>expect(mocks.remove).toHaveBeenCalledWith('river'))
})
it('keeps system layer metadata readonly',async()=>{
 mocks.list.mockResolvedValue({items:[layer('system')],categories:[]});render(<LayerLibrary/>);fireEvent.click(await screen.findByRole('button',{name:/河流线/}));expect(screen.queryByText('编辑元信息')).not.toBeInTheDocument();expect(screen.queryByText('删除登记')).not.toBeInTheDocument();await waitFor(()=>expect(screen.getAllByText('此测试不绘制图片').length).toBeGreaterThan(0))
})
it('scans the migrated folder and imports only selected edited entries',async()=>{
 mocks.scan.mockResolvedValue({path:'/old/China_map',pack:'中国底图',entries:[{manifest:layer(),available_fields:['NAME'],needs_confirmation:true,messages:['坐标系需确认']},{manifest:{...layer(),id:'skip',name:'其他河流',file:'/data/skip.shp'},available_fields:[],needs_confirmation:false}]})
 render(<LayerLibrary/>);fireEvent.click(await screen.findByText('扫描并导入已有数据'));const dialog=screen.getByRole('dialog',{name:'导入内置图层'});expect(within(dialog).getByLabelText('数据文件夹')).toHaveValue('/old/China_map');fireEvent.click(within(dialog).getByText('扫描文件夹'));const first=await within(dialog).findByRole('group',{name:'河流线'});fireEvent.change(within(first).getByLabelText('名称'),{target:{value:'主要河流'}});fireEvent.click(within(dialog).getByLabelText('/data/skip.shp'));fireEvent.click(within(dialog).getByText('确认导入'));await waitFor(()=>expect(mocks.import).toHaveBeenCalledWith('中国底图',[expect.objectContaining({id:'river',name:'主要河流'})]));expect(await screen.findByText('已导入 1 个图层。')).toBeVisible()
})
