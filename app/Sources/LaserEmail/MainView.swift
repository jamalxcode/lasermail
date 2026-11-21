import LaserKit
import SwiftUI

struct MainView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openWindow) private var openWindow
    @State private var columns = NavigationSplitViewVisibility.all

    var body: some View {
        @Bindable var model = model
        NavigationSplitView(columnVisibility: $columns) {
            SidebarView()
                .navigationSplitViewColumnWidth(min: 180, ideal: 220, max: 300)
        } content: {
            MessageListView()
                .navigationSplitViewColumnWidth(min: 280, ideal: 360, max: 520)
        } detail: {
            ReaderView()
        }
        .searchable(text: $model.searchText, placement: .toolbar, prompt: "Search")
        .onChange(of: model.pendingCompose) { _, req in
            if let req { openWindow(id: "compose", value: req); model.pendingCompose = nil }
        }
        .sheet(isPresented: $model.showAddAccount) {
            AddAccountSheet()
        }
        .alert("Laser Email", isPresented: Binding(get: { model.alert != nil }, set: { if !$0 { model.alert = nil } })) {
            Button("OK") { model.alert = nil }
        } message: {
            Text(model.alert ?? "")
        }
        .overlay(alignment: .bottom) {
            if let err = model.connectionError {
                Label(err, systemImage: "exclamationmark.triangle")
                    .padding(8)
                    .background(.regularMaterial, in: Capsule())
                    .padding()
            }
        }
    }
}
