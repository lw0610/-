from pathlib import Path

from autocarto.tools.system._shared.layout_module_loader import load_layout_handler

__all__ = [
    "set_item_background_handler",
    "set_item_frame_handler",
    "set_item_rotation_handler",
    "set_legend_style_handler",
    "set_scalebar_style_handler",
    "set_label_style_handler",
    "set_map_rotation_handler",
    "set_map_layers_handler",
    "set_north_arrow_style_handler",
    "get_item_properties_handler",
    "get_items_properties_handler",
    "export_image_handler",
    "export_svg_handler",
    "set_page_background_handler",
    "distribute_items_handler",
    "group_items_handler",
    "ungroup_items_handler",
    "set_item_z_order_handler",
    "move_callout_handler",
]


def __getattr__(name):
    if name not in __all__:
        raise AttributeError(f"module {__name__!r} has no attribute {name!r}")
    if name == "move_callout_handler":
        from autocarto.tools.system._shared.print_layout_callout_impl import move_callout_handler
        return move_callout_handler
    return load_layout_handler(
        "autocarto.tools.system._shared.print_layout_advanced_impl", name,
        Path(__file__).resolve().parents[2] / "_shared" / "print_layout_advanced_impl.py",
    )
