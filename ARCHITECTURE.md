How are we to actually get things done?

# Manual workflow

1. Human explores and examines patterns in site structure.
2. Creates processes based off:
    1. Site structures.
    2. Allowed / expected behavior on certain structures.
    3. External factors -> API
3. Runs processes .
4. Adapts processes to dynamic changes in the dependencies.

The LLM here, is simply responsible for giving labels to site
structures given their content, and for translating english
instructions into procedure. (like python/code reasoning)

# Slightly different structures

For now, we can deal with the issue of slightly different
structures by abstract over them with LLM. (i.e. creating the same
but slightly different labels for them)

# API

The main workflows involved can largely be divided into:

1. Search / explore.
2. Labelling.
3. Build a workflow.

