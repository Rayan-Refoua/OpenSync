from binaryninja import *

targets = [
    0x140151df0,
    0x140321980,
    0x140322580,
    0x140325190,
]

print("\n=== OPENSYNC TARGETED HLIL ===")

for addr in targets:
    funcs = bv.get_functions_at(addr)

    if not funcs:
        print(f"\nNO FUNCTION: {addr:#x}")
        continue

    f = funcs[0]
    print(f"\n{'=' * 65}")
    print(f"FUNCTION: {f.name} @ {f.start:#x}")
    print(f"SIZE: {f.total_bytes} bytes")

    try:
        lines = list(f.hlil.root.lines)
        print(f"HLIL LINES: {len(lines)}")

        keywords = (
            "allowSyncToSplitTrack",
            "tracksplit",
            "splittrack",
        )

        hits = [
            i for i, line in enumerate(lines)
            if any(k in str(line).lower() for k in
                   [x.lower() for x in keywords])
        ]

        if not hits:
            print("No direct keyword in HLIL.")
            continue

        selected = set()
        for i in hits:
            selected.update(range(max(0, i - 12),
                                  min(len(lines), i + 18)))

        for i in sorted(selected):
            print(f"{i:04d}: {lines[i]}")

    except Exception as e:
        print("HLIL ERROR:", repr(e))

print("\n=== COMPLETE ===")
