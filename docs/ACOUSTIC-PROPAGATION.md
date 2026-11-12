# Acoustic arrival is not independently measured capture time

The Aghaee footage was recorded in a large concert hall; the user reports
some cameras could have been over 60 metres from the sound source. At 343 m/s,
60 metres corresponds to roughly 175 ms. Relative delay depends on the
difference in effective sound paths, not merely one camera's distance.
PA processing, delayed fill speakers and reflections can add other paths.

For a stationary source, a simplified recording model is:

    local event time = physical event time - recording start + path delay

An acoustic match estimates `recording start - path delay`, relative to the
reference microphone. These two quantities cannot be separated from an
unreferenced audio pair alone. A fixed 175ms travel delay is not 175ms of clock
drift, nor evidence that an otherwise good pair should be rejected.

The engine already searches arbitrary start offsets. Its fine residual limits
constrain inconsistency around the fitted acoustic offset, not the offset's
absolute size. Those limits must not be widened simply because a venue is big.
Repeated music and false pairs do not become valid under a hall tolerance.

Saved timing models now explicitly identify acoustic-arrival alignment and
leave propagation delay unknown. Physical capture/video-event alignment is
not independently established. Source timecode is a separate media address;
it does not supply trustworthy simultaneous capture time by itself.

Regression: simulated 175ms propagation plus three reflections and noise must
recover the acoustic offset, retain a unit clock rate, create no unnecessary
audio derivative, and avoid claiming the independently known capture start.

Changing dominant reflections, multiple loudspeaker paths, source movement,
and microphone movement require further path-aware verification. A sequence
of different correlation peaks is not sufficient proof of clock drift. Weak
discovery hypotheses remain available for further evidence; they do not
automatically become precise timeline placements. Any future physical-event
correction must cite independent evidence (for example verified capture
clocks, geometry/path measurements, or an audiovisual event anchor).

`acoustic-relationships.json`, also embedded in the native project's graph,
now retains a per-hypothesis delay profile. Four distinct precise observation
times are required before classifying a trend. Indistinct peaks remain visible
but cannot establish motion. The map describes recording relationships; it
does not assume one recording equals one physical camera. Neither a provisional
relationship nor its diagnostic trend authorizes placement or video retiming.

References:
- https://openstax.org/books/college-physics-ap-courses/pages/17-2-speed-of-sound-frequency-and-wavelength
- https://support.qscaudio.com/sound-advice/sound-advice-%7C-loudspeaker-placement-part-7-using-delayed-loudspeakers

The hall information is a plausible explanation for some timing disagreements,
not proof of the cause or of the true C0559/2144 video offset.
