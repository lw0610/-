import {expect,test} from '@playwright/test'
import {mockDesktopRuntime} from './helpers/desktopRuntime'

test('layer library scans a folder, confirms metadata, and removes only a registration',async({page})=>{
 await mockDesktopRuntime(page,{port:50131})
 let imported=false,deleted=false
 const item={api_version:'builtin-layer/v1',id:'river',name:'河流线',aliases:['水系'],pack:'中国底图',category:'river',geometry:'line',file:'/fixture/river.gpkg',crs:'EPSG:4326',declared_crs:'EPSG:4326',feature_count:12,scope:'user',status:'ready',coverage:{bbox:[110,25,116,30],regions:['湖南']},source:{title:'图层库测试夹具',license:'测试数据'},compliance:'仅用于界面验证'}
 await page.route('**/api/**',async route=>{
  const request=route.request(),path=new URL(request.url()).pathname
  if(request.method()==='OPTIONS'){await route.fulfill({status:204});return}
  if(path.endsWith('/preview')){await route.fulfill({contentType:'image/svg+xml',body:'<svg xmlns="http://www.w3.org/2000/svg" width="400" height="200"><rect width="400" height="200" fill="#f1f5f9"/><path d="M30 170Q160 10 370 80" fill="none" stroke="#3b82f6" stroke-width="5"/></svg>'});return}
  let data:unknown={}
  if(path==='/api/runtime/snapshot'||path==='/api/runtime/services')data={services:{},instances:{items:[],active_id:null}}
  else if(path==='/api/guides')data=[]
  else if(path==='/api/agent-configs')data={configs:[]}
  else if(path==='/api/layers')data={items:imported&&!deleted?[item]:[],categories:[{id:'river',name:'河流'}],migration_suggestions:[]}
  else if(path==='/api/layers/import/scan')data={path:'/fixture',pack:'中国底图',entries:[{manifest:item,available_fields:['NAME'],needs_confirmation:true,messages:['请确认坐标系']}]}
  else if(path==='/api/layers/import/confirm'){expect(request.postDataJSON().entries[0].name).toBe('河流线');imported=true;data={items:[item]}}
  else if(path==='/api/layers/river'&&request.method()==='DELETE'){deleted=true;data={deleted:true}}
  await route.fulfill({json:{success:true,data}})
 })
 await page.goto('/guides');await page.getByRole('button',{name:'内置图层',exact:true}).click();await expect(page.getByRole('button',{name:'内置图层',exact:true})).toHaveAttribute('aria-pressed','true')
 await page.getByText('导入图层文件夹').click();await page.getByLabel('数据文件夹').fill('/fixture');await page.getByText('扫描文件夹',{exact:true}).click();await expect(page.getByLabel('确认坐标系')).toHaveValue('EPSG:4326');expect(imported).toBe(false);await page.getByText('确认导入',{exact:true}).click();await expect(page.getByRole('status')).toContainText('已导入 1 个图层')
 await page.getByRole('button',{name:/河流线/}).click();await expect(page.getByRole('complementary',{name:'图层详情'})).toContainText('EPSG:4326');await expect(page.getByRole('img',{name:'河流线图层预览'}).first()).toBeVisible()
 await page.screenshot({path:'../docs/refactor-v7/evidence/D12-layer-library.png',fullPage:true})
 await page.emulateMedia({colorScheme:'dark'});await page.screenshot({path:'../docs/refactor-v7/evidence/D12-layer-library-dark.png',fullPage:true});await page.emulateMedia({colorScheme:'light'})
 await page.getByText('删除登记',{exact:true}).click();expect(deleted).toBe(false);await page.getByText('确认删除登记',{exact:true}).click();await expect.poll(()=>deleted).toBe(true);await expect(page.getByRole('status')).toContainText('原始文件保留')
})
