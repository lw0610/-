from pathlib import Path

from autocarto.tools.system._shared.layout_module_loader import load_layout_handler

__all__ = [
    "add_map_item_handler",
    "set_map_extent_handler",
    "add_legend_handler",
    "add_scalebar_handler",
    "add_label_handler",
    "add_picture_handler",
    "add_shape_handler",
    "remove_item_handler",
    "move_item_handler",
    "resize_item_handler",
    "add_north_arrow_handler",
    "add_grid_handler",
    "add_attribute_table_handler",
    "lock_item_handler",
    "align_items_handler",
    "add_overview_handler",
    "add_decorative_border_handler",
    "add_file_overview_to_layout_handler",
]


def __getattr__(name):
    if name not in __all__:
        raise AttributeError(f"module {__name__!r} has no attribute {name!r}")
    if name == "add_file_overview_to_layout_handler":
        from autocarto.tools.system._shared.layout_file_overview_impl import add_file_overview_to_layout_handler
        return add_file_overview_to_layout_handler
    return load_layout_handler(
        "autocarto.tools.system._shared.print_layout_items_impl", name,
        Path(__file__).resolve().parents[2] / "_shared" / "print_layout_items_impl.py",
    )
