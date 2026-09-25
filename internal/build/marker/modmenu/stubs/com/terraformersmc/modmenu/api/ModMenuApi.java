package com.terraformersmc.modmenu.api;

import java.util.Map;
import java.util.function.Consumer;

public interface ModMenuApi {
	default UpdateChecker getUpdateChecker() {
		return null;
	}

	default Map<String, UpdateChecker> getProvidedUpdateCheckers() {
		return Map.of();
	}

	default void attachModpackBadges(Consumer<String> consumer) {
	}
}
