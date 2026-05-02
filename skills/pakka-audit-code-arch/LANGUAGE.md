# Module Language

Use these terms exactly in every suggestion. No substitutes.

| Term | Meaning | Avoid |
|---|---|---|
| **Module** | Anything with an interface and an implementation. Scale-agnostic: a function, class, package, or tier-spanning slice. | unit, component, service |
| **Interface** | Everything a caller must know: type signature, invariants, ordering constraints, error modes, config, performance characteristics. | API, signature (too narrow) |
| **Implementation** | What's inside the module. | — |
| **Depth** | Leverage at the interface. Deep = large behavior behind small interface. Shallow = interface nearly as complex as implementation. | — |
| **Seam** | Where a module's interface lives. Where you can alter behavior without editing in place. | boundary (overloaded with DDD) |
| **Adapter** | A concrete thing satisfying an interface at a seam. Describes role, not substance. | — |
| **Leverage** | What callers get from depth: more capability per unit of interface learned. | — |
| **Locality** | What maintainers get from depth: change, bugs, and knowledge concentrate at one place rather than spreading across callers. | — |

## Principles

**Deletion test.** Imagine deleting the module. Complexity vanishes → pass-through, not earning its keep. Complexity reappears across N callers → it was earning its keep.

**Interface is the test surface.** Callers and tests cross the same seam. Testing past the interface → module is the wrong shape.

**One adapter = hypothetical seam. Two adapters = real seam.** Don't introduce a seam until something actually varies across it.

**Depth is a property of the interface, not the implementation.** A deep module can have internal seams (private, used only by its own tests) as well as the external seam at its interface. Internal complexity doesn't make an interface deep.
