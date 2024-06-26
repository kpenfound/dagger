defmodule Dagger.Mod.Function do
  @moduledoc false

  alias Dagger.Mod.Helper

  @doc """
  Define a Dagger function.
  """
  def define(dag, {name, fun_def}, doc_content) do
    args = fun_def[:args] || []
    return = Keyword.fetch!(fun_def, :return)

    dag
    |> Dagger.Client.function(
      Helper.camelize(name),
      define_type(dag, Dagger.Client.type_def(dag), return)
    )
    |> maybe_with_description(doc_content)
    |> with_args(args, dag)
  end

  defp maybe_with_description(function, doc) when doc in [:none, :hidden], do: function

  defp maybe_with_description(function, %{"en" => doc}) do
    Dagger.Function.with_description(function, doc)
  end

  defp with_args(fun_def, args, dag) do
    args
    |> Enum.reduce(fun_def, fn {name, info}, fun_def ->
      type = Keyword.fetch!(info, :type)

      type_def =
        define_type(dag, Dagger.Client.type_def(dag), type)
        |> then(&maybe_with_optional(&1, info[:optional]))

      fun_def
      |> Dagger.Function.with_arg(name, type_def)
    end)
  end

  defp maybe_with_optional(type_def, optional?) do
    if optional? do
      Dagger.TypeDef.with_optional(type_def, true)
    else
      type_def
    end
  end

  # TODO: support type list.

  defp define_type(_dag, type_def, :integer) do
    type_def
    |> Dagger.TypeDef.with_kind(Dagger.TypeDefKind.integer_kind())
  end

  defp define_type(_dag, type_def, :boolean) do
    type_def
    |> Dagger.TypeDef.with_kind(Dagger.TypeDefKind.boolean_kind())
  end

  defp define_type(_dag, type_def, :string) do
    type_def
    |> Dagger.TypeDef.with_kind(Dagger.TypeDefKind.string_kind())
  end

  defp define_type(dag, type_def, {:list, type}) do
    type_def
    |> Dagger.TypeDef.with_list_of(define_type(dag, Dagger.Client.type_def(dag), type))
  end

  defp define_type(_dag, type_def, module) do
    case Module.split(module) do
      # A module that generated by codegen.
      ["Dagger", name] ->
        type_def
        |> Dagger.TypeDef.with_object(name)

      [name] ->
        type_def
        |> Dagger.TypeDef.with_object(name)
    end
  end
end
