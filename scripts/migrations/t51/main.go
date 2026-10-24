//go:build ignore

package main

import (
 "encoding/json"
 "fmt"
 "os"
 "path/filepath"
 "reflect"
 "sort"
 "github.com/autocarto/carto_agent/internal/config"
 "github.com/autocarto/carto_agent/internal/flow"
 "github.com/autocarto/carto_agent/internal/resource/bindings"
 "gopkg.in/yaml.v3"
)

func die(err error){if err!=nil {fmt.Fprintln(os.Stderr,err);os.Exit(1)}}
func object(v any) map[string]any {m,_:=v.(map[string]any);return m}
func keys(v any)[]string {result:=[]string{};for k:=range object(v){result=append(result,k)};sort.Strings(result);return result}
func main(){
 root:=os.Getenv("T51_PROJECT_ROOT");if root==""{die(fmt.Errorf("T51_PROJECT_ROOT required"))}
 if len(os.Args)>1&&os.Args[1]=="binding-check"{
  key:="flow:marching-route/"+flow.ResourceKey("data_list","中国底图")
  resolved,found,err:=bindings.New(filepath.Join(root,"autocarto_data/config/resource_bindings.yaml")).Get(key);die(err)
  expected:=filepath.Clean(filepath.Join(root,"../数据/底图数据/China_map"))
  if !found||resolved!=expected{die(fmt.Errorf("binding did not resolve from project root"))}
  fmt.Println(`{"binding_key_verified":true,"relative_path_resolved":true}`);return
 }
 raw,err:=os.ReadFile(filepath.Join(root,"carto_agent/internal/config/testdata/v1_default_agent.yaml"));die(err)
 var public map[string]any;die(yaml.Unmarshal(raw,&public))
 paths,err:=filepath.Glob(filepath.Join(root,"autocarto_data/config/agent_configs/*/agent.yaml"));die(err)
 if len(os.Args)>1 && (os.Args[1]=="verify-stage" || os.Args[1]=="verify-real"){
  verify(root,paths,os.Args[1]=="verify-real");return
 }
 if len(os.Args)>1 && (os.Args[1]=="stage" || os.Args[1]=="persist"){
  migrate(root,paths,os.Args[1]=="persist");return
 }
 for _,path:=range paths{
  raw,err:=os.ReadFile(path);die(err);var doc map[string]any;die(yaml.Unmarshal(raw,&doc))
  agents:=object(object(doc["runtime"])["agents"]);originals:=object(object(public["runtime"])["agents"])
  roles:=[]map[string]any{}
  for _,name:=range keys(agents){
   role,original:=object(agents[name]),object(originals[name])
   roles=append(roles,map[string]any{"id":name,"display_name":role["display_name"],"enabled":role["enabled"],"kind":role["kind"],"tool_packages":role["tool_packages"],"legacy_packages":role["skill_packages"],"prompt_keys":keys(role["prompts"]),"prompts_equal_public_v1":reflect.DeepEqual(role["prompts"],original["prompts"]),"llm_keys":keys(role["llm"]),"provider_equal_public_v1":reflect.DeepEqual(role["llm"],original["llm"])})
  }
  output:=map[string]any{"scheme":filepath.Base(filepath.Dir(path)),"version":doc["version"],"top_level_keys":keys(doc),"roles":roles}
  planSummary:=[]map[string]any{}
  for _,name:=range keys(object(doc["plan"])["agents"]){role:=object(object(object(doc["plan"])["agents"])[name]);planSummary=append(planSummary,map[string]any{"id":name,"display_name":role["display_name"],"is_main":role["is_main"],"legacy_packages":role["skill_packages"],"keys":keys(role),"prompt_keys":keys(role["prompts"])})}
  if filepath.Base(filepath.Dir(path))=="专题图制作" {output["plan_roles"]=planSummary;output["plan_keys"]=keys(doc["plan"]);output["default_execution_mode"]=object(doc["meta"])["default_execution_mode"]}
  encoded,err:=json.Marshal(output);die(err);fmt.Println(string(encoded))
 }
}
func samePackages(a,b []string) bool {left,right:=append([]string{},a...),append([]string{},b...);sort.Strings(left);sort.Strings(right);return reflect.DeepEqual(left,right)}
func migrate(root string,paths []string,persist bool){
 stageDir:=filepath.Join(root,"autocarto/tests/golden/.runs/T51-migration-stage/agent_configs")
 targetRaw,err:=os.ReadFile(filepath.Join(root,"carto_agent/defaults/agent_configs/default/agent.yaml"));die(err)
 var target,old config.AgentConfigFile;die(yaml.Unmarshal(targetRaw,&target))
 oldRaw,err:=os.ReadFile(filepath.Join(root,"carto_agent/internal/config/testdata/v1_default_agent.yaml"));die(err);die(yaml.Unmarshal(oldRaw,&old))
 repaired:=false
 if !persist {
  path:=filepath.Join(stageDir,"专题图制作/agent.yaml");raw,err:=os.ReadFile(path);die(err)
  var incomplete config.AgentConfigFile;die(yaml.Unmarshal(raw,&incomplete))
  if incomplete.Runtime==nil {
   incomplete.Runtime=target.Runtime
   candidate,err:=yaml.Marshal(&incomplete);die(err);die(os.WriteFile(path,candidate,0600));repaired=true
  }
 }
 stage:=config.NewAgentConfigManager(stageDir)
 oldProjection:=config.ConvertToRuntimeSkillConfigWithPlugin(&old,filepath.Join(root,"autocarto/skills/system"),"","",filepath.Join(root,"autocarto_data/config"),"default")
 var real *config.AgentConfigManager
 if persist {real=config.NewAgentConfigManager(filepath.Join(root,"autocarto_data/config/agent_configs"))}
 for _,path:=range paths{
  name:=filepath.Base(filepath.Dir(path));preview,err:=stage.PreviewMigration(name);die(err)
  changes:=[]string{}
  if !persist && name=="default"{
   sourceRaw,err:=os.ReadFile(path);die(err);var source config.AgentConfigFile;die(yaml.Unmarshal(sourceRaw,&source))
   for id,role:=range preview.Config.Runtime.Agents{
    base,ok:=old.Runtime.Agents[id];if !ok {continue};desired,ok:=target.Runtime.Agents[id];if !ok{continue}
    if reflect.DeepEqual(role.Prompts,base.Prompts){role.Prompts=desired.Prompts;changes=append(changes,"runtime."+id+".prompts")}
    if role.Description==base.Description{role.Description=desired.Description}
    if samePackages(role.ToolPackages,oldProjection.Agents[id].ToolPackages){role.ToolPackages=desired.ToolPackages;changes=append(changes,"runtime."+id+".tool_packages")}
    preview.Config.Runtime.Agents[id]=role
   }
   if source.Flows==nil{preview.Config.Flows=target.Flows;changes=append(changes,"flows.enabled")}
   if preview.Config.Plan!=nil && old.Plan!=nil && target.Plan!=nil{
    for id,role:=range preview.Config.Plan.Agents{base,ok:=old.Plan.Agents[id];if !ok{continue};desired,ok:=target.Plan.Agents[id];if !ok{continue};if reflect.DeepEqual(role.Prompts,base.Prompts){role.Prompts=desired.Prompts;changes=append(changes,"plan."+id+".prompts")};preview.Config.Plan.Agents[id]=role}
    if samePackages(source.Plan.Global.SkillPackages,old.Plan.Global.SkillPackages){preview.Config.Plan.Global=target.Plan.Global;changes=append(changes,"plan.global.tool_packages")}
   }
  }
  manager:=stage;if persist{manager=real}
  die(manager.SaveConfig("",name,preview.Config))
  output:=map[string]any{"scheme":name,"changed":preview.Changed,"warnings":preview.Warnings,"saved_v2":true,"updated_default_fields":changes,"plan_only_runtime_supplemented":name=="专题图制作"&&(repaired||persist)}
  encoded,err:=json.Marshal(output);die(err);fmt.Println(string(encoded))
 }
 if persist{
  reopened:=config.NewAgentConfigManager(filepath.Join(root,"autocarto_data/config/agent_configs"))
  for _,path:=range paths{name:=filepath.Base(filepath.Dir(path));data,err:=reopened.GetConfigData(name);die(err);if data.Version!="2.0"{die(fmt.Errorf("v2 not saved for %s",name))}}
 }
}
func verify(root string,paths []string,real bool){
 backupRaw,err:=os.ReadFile(filepath.Join(root,"autocarto/tests/golden/.runs/T51-backup-summary.json"));die(err)
 var backup struct{Path string `json:"backup_path"`};die(json.Unmarshal(backupRaw,&backup))
 targetRaw,err:=os.ReadFile(filepath.Join(root,"carto_agent/defaults/agent_configs/default/agent.yaml"));die(err)
 var target config.AgentConfigFile;die(yaml.Unmarshal(targetRaw,&target))
 for _,path:=range paths{
  name:=filepath.Base(filepath.Dir(path));beforeRaw,err:=os.ReadFile(filepath.Join(backup.Path,"agent_configs",name,"agent.yaml"));die(err)
  var before config.AgentConfigFile;die(yaml.Unmarshal(beforeRaw,&before))
  afterPath:=filepath.Join(root,"autocarto/tests/golden/.runs/T51-migration-stage/agent_configs",name,"agent.yaml");if real{afterPath=path}
  afterRaw,err:=os.ReadFile(afterPath);die(err);var after config.AgentConfigFile;die(yaml.Unmarshal(afterRaw,&after))
  if after.Version!="2.0"||after.Runtime==nil{die(fmt.Errorf("invalid migrated scheme %s",name))}
  preserved:=true
  if before.Runtime!=nil{
   if len(before.Runtime.Agents)!=len(after.Runtime.Agents){die(fmt.Errorf("custom role count changed: %s",name))}
   for id,role:=range before.Runtime.Agents{
    migrated:=after.Runtime.Agents[id]
    if role.Enabled!=migrated.Enabled||role.LLM!=migrated.LLM||role.DisplayName!=migrated.DisplayName{die(fmt.Errorf("role settings changed: %s/%s",name,id))}
    if name!="default"&&!reflect.DeepEqual(role.Prompts,migrated.Prompts){die(fmt.Errorf("custom prompt changed: %s/%s",name,id))}
    if name=="default"&&(!reflect.DeepEqual(migrated.Prompts,target.Runtime.Agents[id].Prompts)||!samePackages(migrated.ToolPackages,target.Runtime.Agents[id].ToolPackages)){die(fmt.Errorf("default target not reached: %s",id))}
   }
  }
  if name!="default"&&before.Plan!=nil{
   if after.Plan==nil||len(before.Plan.Agents)!=len(after.Plan.Agents){die(fmt.Errorf("Plan lost: %s",name))}
   for id,role:=range before.Plan.Agents{migrated:=after.Plan.Agents[id];if !reflect.DeepEqual(role.Prompts,migrated.Prompts)||role.LLM!=migrated.LLM{die(fmt.Errorf("custom Plan changed: %s/%s",name,id))}}
  }
  fmt.Printf("{\"scheme\":%q,\"custom_fields_preserved\":%v,\"runtime_roles\":%d,\"v2\":true}\n",name,preserved,len(after.Runtime.Agents))
 }
}
