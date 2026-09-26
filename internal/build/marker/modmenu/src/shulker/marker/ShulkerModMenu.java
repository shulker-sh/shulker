package shulker.marker;

import com.terraformersmc.modmenu.api.ModMenuApi;
import com.terraformersmc.modmenu.api.UpdateChannel;
import com.terraformersmc.modmenu.api.UpdateChecker;
import com.terraformersmc.modmenu.api.UpdateInfo;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.function.Consumer;

public final class ShulkerModMenu implements ModMenuApi {
	static final String MODS_RESOURCE = "/shulker/mods.txt";

	private static final UpdateInfo NO_UPDATE = new UpdateInfo() {
		@Override
		public boolean isUpdateAvailable() {
			return false;
		}

		@Override
		public String getDownloadLink() {
			return null;
		}

		@Override
		public UpdateChannel getUpdateChannel() {
			return UpdateChannel.RELEASE;
		}
	};

	private static final UpdateChecker MANAGED_BY_SHULKER = () -> NO_UPDATE;

	@Override
	public void attachModpackBadges(Consumer<String> consumer) {
		managedMods().forEach(consumer);
	}

	@Override
	public Map<String, UpdateChecker> getProvidedUpdateCheckers() {
		Map<String, UpdateChecker> checkers = new HashMap<>();

		for (String id : managedMods()) {
			checkers.put(id, MANAGED_BY_SHULKER);
		}

		return checkers;
	}

	static List<String> managedMods() {
		List<String> ids = new ArrayList<>();

		try (InputStream in = ShulkerModMenu.class.getResourceAsStream(MODS_RESOURCE)) {
			if (in == null) {
				return ids;
			}

			BufferedReader reader = new BufferedReader(new InputStreamReader(in, StandardCharsets.UTF_8));
			String line;

			while ((line = reader.readLine()) != null) {
				line = line.trim();

				if (!line.isEmpty()) {
					ids.add(line);
				}
			}
		} catch (IOException ignored) {
		}

		return ids;
	}
}
