from binaryninja import *

setter = 0x140325190

print("=== TRACK SPLIT SETTING CONSUMERS ===")
print("Setter:", hex(setter))

refs = list(bv.get_code_refs(setter))

print("Setter references:", len(refs))

for ref in refs:
    f = ref.function
    if f is None:
        continue

    print(f"\nCALL SITE: {ref.address:#x}")
    print(f"FUNCTION: {f.name} @ {f.start:#x}")

    try:
        lines = list(f.hlil.root.lines)

        for i, line in enumerate(lines):
            if "sub_140325190" in str(line):
                lo = max(0, i - 15)
                hi = min(len(lines), i + 16)

                for j in range(lo, hi):
                    print(f"{j:04d}: {lines[j]}")
    except Exception as e:
        print("ERROR:", repr(e))

print("\n=== COMPLETE ===")
