from binaryninja import *

bv = current_view
if bv is None:
    raise RuntimeError("Open PEServer.exe in Binary Ninja first")

targets = [
    "tracksSplitByDevice",
    "tracksSplitByTakes",
    "allowSyncToSplitTrack",
    "pe.session.sync.tracksSplit",
]

print("\n=== OPENSYNC TRACK LOGIC INVESTIGATION ===")
print("Binary:", bv.file.filename)
print("Functions:", len(bv.functions))

for target in targets:
    print(f"\n=== {target} ===")
    matches = [
        s for s in bv.get_strings()
        if target.lower() in str(s.value).lower()
    ]

    print("String matches:", len(matches))

    for s in matches[:10]:
        print(f"\nSTRING @ {s.start:#x}: {s.value!r}")
        refs = list(bv.get_code_refs(s.start))
        print("Direct code references:", len(refs))

        seen = set()
        for ref in refs[:20]:
            func = ref.function
            if func is None:
                continue

            print(f"  XREF {ref.address:#x} -> {func.name} @ {func.start:#x}")

            if func.start in seen:
                continue
            seen.add(func.start)

            callers = list(bv.get_code_refs(func.start))
            print("  References to containing function:", len(callers))

            for caller in callers[:12]:
                owner = caller.function
                name = owner.name if owner else "UNKNOWN"
                print(f"    {caller.address:#x}: {name}")

print("\n=== INSPECTION COMPLETE ===")
