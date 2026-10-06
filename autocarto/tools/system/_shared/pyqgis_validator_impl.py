import ast
import logging
from dataclasses import dataclass
from typing import Any, Dict, List, Optional

logger = logging.getLogger(__name__)

FORBIDDEN_MODULES = {
    "subprocess",
    "shutil",
    "sys",
    "os",
    "pathlib",
    "importlib",
    "ctypes",
    "socket",
    "urllib",
    "requests",
    "http",
    "ftplib",
    "smtplib",
    "telnetlib",
    "pickle",
    "marshal",
    "shelve",
    "__builtin__",
    "builtins",
}

FORBIDDEN_FUNCTIONS = {
    "eval",
    "exec",
    "compile",
    "open",
    "__import__",
    "globals",
    "locals",
    "getattr",
    "setattr",
    "delattr",
    "input",
    "raw_input",
}

ALLOWED_QGIS_MODULES = {
    "qgis.core",
    "qgis.gui",
    "qgis.analysis",
    "qgis.utils",
    "processing",
}


@dataclass
class ValidationResult:
    is_valid: bool
    errors: List[str]
    warnings: List[str]

    def to_dict(self) -> Dict[str, Any]:
        return {
            "is_valid": self.is_valid,
            "errors": self.errors,
            "warnings": self.warnings,
        }


class ScriptValidator:
    def __init__(self):
        self._forbidden_modules = FORBIDDEN_MODULES.copy()
        self._forbidden_functions = FORBIDDEN_FUNCTIONS.copy()
        self._allowed_qgis_modules = ALLOWED_QGIS_MODULES.copy()

    def validate(self, script: str) -> ValidationResult:
        errors: List[str] = []
        warnings: List[str] = []

        syntax_result = self._check_syntax(script)
        if syntax_result:
            errors.append(syntax_result)
            return ValidationResult(False, errors, warnings)

        try:
            tree = ast.parse(script)
            self._check_ast(tree, errors, warnings)
        except Exception as exc:
            errors.append(f"AST parse failed: {exc}")

        return ValidationResult(len(errors) == 0, errors, warnings)

    def _check_syntax(self, script: str) -> Optional[str]:
        try:
            compile(script, "<script>", "exec")
            return None
        except SyntaxError as exc:
            return f"Syntax error (line {exc.lineno}): {exc.msg}"

    def _check_ast(self, tree: ast.AST, errors: List[str], warnings: List[str]) -> None:
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                for alias in node.names:
                    self._check_import(alias.name, errors, warnings)
            elif isinstance(node, ast.ImportFrom):
                if node.module:
                    self._check_import(node.module, errors, warnings)
            elif isinstance(node, ast.Call):
                self._check_call(node, errors, warnings)
            elif isinstance(node, ast.For):
                self._check_dangerous_layer_mutation_loop(node, errors)

    def _check_import(
        self, module_name: str, errors: List[str], warnings: List[str]
    ) -> None:
        base_module = module_name.split(".")[0]

        if base_module in self._forbidden_modules:
            errors.append(f"Forbidden module import: {module_name}")
            return

        is_qgis = any(module_name.startswith(mod) for mod in self._allowed_qgis_modules)
        if not is_qgis and base_module not in {"math", "json", "re", "datetime"}:
            warnings.append(f"Non-standard module: {module_name}")

    def _check_call(
        self, node: ast.Call, errors: List[str], warnings: List[str]
    ) -> None:
        _ = warnings
        func_name = None

        if isinstance(node.func, ast.Name):
            func_name = node.func.id
        elif isinstance(node.func, ast.Attribute):
            func_name = node.func.attr

        if func_name and func_name in self._forbidden_functions:
            errors.append(f"Forbidden function call: {func_name}")

    @staticmethod
    def _is_map_layers_view_iteration_expr(node: ast.AST) -> bool:
        if not isinstance(node, ast.Call):
            return False
        if not isinstance(node.func, ast.Attribute):
            return False
        if node.func.attr not in {"values", "items", "keys"}:
            return False

        parent_call = node.func.value
        if not isinstance(parent_call, ast.Call):
            return False
        if not isinstance(parent_call.func, ast.Attribute):
            return False
        return parent_call.func.attr == "mapLayers"

    @classmethod
    def _is_live_map_layers_iteration(cls, node: ast.AST) -> bool:
        if cls._is_map_layers_view_iteration_expr(node):
            return True

        # list(...) / tuple(...) means caller already created a safe snapshot.
        if isinstance(node, ast.Call) and isinstance(node.func, ast.Name):
            if node.func.id in {"list", "tuple"} and node.args:
                return False

        return False

    @staticmethod
    def _contains_layer_removal_call(loop_node: ast.For) -> bool:
        for child in ast.walk(loop_node):
            if not isinstance(child, ast.Call):
                continue
            if isinstance(child.func, ast.Attribute) and child.func.attr in {
                "removeMapLayer",
                "removeMapLayers",
                "removeAllMapLayers",
            }:
                return True
        return False

    def _check_dangerous_layer_mutation_loop(
        self, node: ast.For, errors: List[str]
    ) -> None:
        if not self._is_live_map_layers_iteration(node.iter):
            return
        if not self._contains_layer_removal_call(node):
            return
        errors.append(
            "Dangerous script pattern: do not remove project layers while iterating "
            "project.mapLayers().values()/items()/keys(); use a snapshot first, "
            "for example: for layer in list(project.mapLayers().values())."
        )
