"""Private named presets remain separate from the two public resource types."""
from pathlib import Path
import pytest
from autocarto.tools.system._shared.tool_presets import resolve_tool_preset
from autocarto.tests.support.resource_handles import load_handle


@pytest.mark.parametrize('name,fixture',[('单色','mono'),('红蓝对比','red-blue')])
def test_named_route_preset_matches_previous_style_contract(tmp_path,monkeypatch,name,fixture):
    monkeypatch.setenv('AUTOCARTO_HOME',str(tmp_path))
    actual=resolve_tool_preset(name,'route_style')['entries'];expected=load_handle(fixture)['entries']
    keys=('topology','side','route_type','draw_type','style_id','width_scale','legend_label','preset_ref','default_sides','reference_style_id','label_paths','auto_width_group')
    assert [{k:r[k] for k in keys} for r in actual]==[{k:r[k] for k in keys} for r in expected]


def test_named_symbols_resolve_files_and_user_ambiguity_is_rejected(tmp_path,monkeypatch):
    monkeypatch.setenv('AUTOCARTO_HOME',str(tmp_path))
    actual=resolve_tool_preset('军标','symbols');assert all(Path(r['path']).is_file() for r in actual['entries'])
    folder=tmp_path/'presets/route-cartography/symbols/军标';folder.mkdir(parents=True)
    with pytest.raises(ValueError,match='不唯一'):resolve_tool_preset('军标','symbols')
    for name in ('../军标','/tmp/军标','军标/point.svg'):
        with pytest.raises(ValueError):resolve_tool_preset(name,'symbols')


def test_normalized_migrated_csv_and_symlink_files(tmp_path,monkeypatch):
    monkeypatch.setenv('AUTOCARTO_HOME',str(tmp_path))
    root=tmp_path/'presets/route-cartography/styles';root.mkdir(parents=True)
    (root/'自定义.csv').write_text('topology,side,route_type,draw_type,style_id,width_scale,legend_label,preset_ref,default_sides,reference_style_id,label_paths,auto_width_group\nstandard,neutral,route,bezier_arrow,red,1.25,路线,mine,friendly、neutral,ref,true,false\n')
    item=resolve_tool_preset('自定义','route_style')['entries'][0]
    assert item['style_id']=='red' and item['width_scale']==1.25 and item['label_paths'] and item['default_sides']==['friendly','neutral']
    (root/'链接.csv').symlink_to(root/'自定义.csv')
    with pytest.raises(ValueError,match='符号链接'):resolve_tool_preset('链接','route_style')
